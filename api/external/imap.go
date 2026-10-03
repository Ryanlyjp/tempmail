package external

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/responses"
)

type xoauth struct{ user, token string }

func (x xoauth) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}
func (x xoauth) Next([]byte) ([]byte, error) {
	return nil, errors.New("OAuth IMAP 授权失败，请检查权限和账号")
}

type rawCommand struct{ cmd *imap.Command }

func (r rawCommand) Command() *imap.Command { return r.cmd }

func gmailSearch(c *client.Client, query string) (map[uint32]bool, error) {
	resp := new(responses.Search)
	status, err := c.Execute(rawCommand{&imap.Command{Name: "UID SEARCH", Arguments: []interface{}{imap.RawString("X-GM-RAW"), query}}}, resp)
	if err != nil {
		return nil, err
	}
	if err = status.Err(); err != nil {
		return nil, err
	}
	out := map[uint32]bool{}
	for _, id := range resp.Ids {
		out[id] = true
	}
	return out, nil
}
func (s *Service) imapSync(ctx context.Context, a Account, run string, first, test bool) error {
	token := ""
	if a.Provider == "outlook" {
		var err error
		token, err = s.oauth(ctx, &a, "https://outlook.office.com/IMAP.AccessAsUser.All offline_access")
		if err != nil {
			return err
		}
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: a.Host, MinVersion: tls.VersionTLS12}}
	conn, err := tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(a.Host, strconv.Itoa(a.Port)))
	if err != nil {
		return fmt.Errorf("IMAP 连接失败: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, err := client.New(conn)
	if err != nil {
		return err
	}
	c.Timeout = 45 * time.Second
	if a.Provider == "outlook" {
		err = c.Authenticate(xoauth{a.Address, token})
	} else {
		err = c.Login(a.Address, a.Credentials.Password)
	}
	if err != nil {
		return errors.New("IMAP 登录失败，请检查授权码、应用密码、OAuth 权限或服务开关")
	}
	defer c.Logout()
	// NetEase requires client identification on some accounts.
	if a.Provider == "163" || a.Provider == "126" {
		_, _ = c.Execute(rawCommand{&imap.Command{Name: "ID", Arguments: []interface{}{[]interface{}{"name", "TempMail", "version", "1.0", "vendor", "TempMail"}}}}, nil)
	}
	if test {
		_, err = c.Select("INBOX", true)
		return err
	}
	folders := make(chan *imap.MailboxInfo, 100)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", folders) }()
	list := []*imap.MailboxInfo{}
	for folder := range folders {
		list = append(list, folder)
	}
	if err = <-done; err != nil {
		return err
	}
	for _, folder := range list {
		if excludedFolder(folder.Name, folder.Attributes) {
			continue
		}
		status, err := c.Select(folder.Name, true)
		if err != nil {
			return fmt.Errorf("无法只读打开文件夹 %s", folder.Name)
		}
		criteria := imap.NewSearchCriteria()
		criteria.Since = a.Since.Add(-24 * time.Hour)
		criteria.WithoutFlags = []string{imap.DeletedFlag, imap.DraftFlag}
		ids, err := c.UidSearch(criteria)
		if err != nil {
			return err
		}
		allowed := map[uint32]bool{}
		promotions := map[uint32]bool{}
		if a.Provider == "gmail" {
			allowed, err = gmailSearch(c, "after:"+strconv.FormatInt(a.Since.Unix(), 10)+" -in:sent -in:drafts -in:trash")
			if err != nil {
				return errors.New("Gmail 收件分类查询失败，停止同步以免混入发件")
			}
			promotions, err = gmailSearch(c, "category:promotions after:"+strconv.FormatInt(a.Since.Unix(), 10))
			if err != nil {
				return errors.New("Gmail 促销分类查询失败，本轮不发送提醒")
			}
		}
		for _, uid := range ids {
			if err = ctx.Err(); err != nil {
				return err
			}
			if a.Provider == "gmail" && !allowed[uid] {
				continue
			}
			ref := fmt.Sprintf("imap:%s:%d:%d", folder.Name, status.UidValidity, uid)
			found, err := s.known(ctx, a, ref, run, promotions[uid])
			if err != nil {
				return err
			}
			if found {
				continue
			}
			set := new(imap.SeqSet)
			set.AddNum(uid)
			section := &imap.BodySectionName{Peek: true}
			items := []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, section.FetchItem()}
			if a.Provider == "gmail" {
				items = append(items, imap.FetchItem("X-GM-MSGID"))
			}
			messages := make(chan *imap.Message, 1)
			go func() { done <- c.UidFetch(set, items, messages) }()
			var ingestErr error
			for msg := range messages {
				body := msg.GetBody(section)
				if body == nil {
					ingestErr = errors.New("IMAP 返回了空邮件正文")
					continue
				}
				raw, e := io.ReadAll(body)
				if e != nil {
					ingestErr = e
					continue
				}
				stable := ""
				if a.Provider == "gmail" {
					v := fmt.Sprint(msg.Items[imap.FetchItem("X-GM-MSGID")])
					if v != "" && v != "<nil>" {
						stable = "gmail:" + strings.TrimSpace(v)
					}
				}
				ingestErr = s.ingest(ctx, a, ref, stable, run, raw, msg.InternalDate, promotions[uid], first)
			}
			if err = <-done; err != nil {
				return err
			}
			if ingestErr != nil {
				return ingestErr
			}
		}
	}
	return nil
}
