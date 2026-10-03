package external

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var remoteHTTP = &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

func (s *Service) oauth(ctx context.Context, a *Account, scope string) (string, error) {
	form := url.Values{"client_id": {a.Credentials.ClientID}, "refresh_token": {a.Credentials.RefreshToken}, "grant_type": {"refresh_token"}, "scope": {scope}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://login.microsoftonline.com/"+a.Authority+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := remoteHTTP.Do(req)
	if err != nil {
		return "", errors.New("微软 Token 服务连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("微软授权失败（HTTP %d），请检查账号类型、Client ID、Refresh Token 和已授权权限", resp.StatusCode)
	}
	var data struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.Access == "" {
		return "", errors.New("微软没有返回 Access Token")
	}
	if data.Refresh != "" && data.Refresh != a.Credentials.RefreshToken {
		a.Credentials.RefreshToken = data.Refresh
		if a.ID != "" {
			b, err := s.seal(a.ID, a.Credentials)
			if err != nil {
				return "", err
			}
			if _, err = s.db.Exec(ctx, `UPDATE external_accounts SET credentials=$2 WHERE id=$1`, a.ID, b); err != nil {
				return "", err
			}
		}
	}
	return data.Access, nil
}
func graphGet(ctx context.Context, token, path string) ([]byte, error) {
	if !strings.HasPrefix(path, "https://graph.microsoft.com/v1.0/") {
		return nil, errors.New("无效的 Graph 分页地址")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", `IdType="ImmutableId"`)
	resp, err := remoteHTTP.Do(req)
	if err != nil {
		return nil, errors.New("Graph 连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Graph 读取失败（HTTP %d），请检查 Mail.Read 权限", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return b, err
}
func (s *Service) graphSync(ctx context.Context, a Account, run string, first bool) error {
	token, err := s.oauth(ctx, &a, "https://graph.microsoft.com/.default offline_access")
	if err != nil {
		return err
	}
	excluded := map[string]bool{}
	var excludeTree func(string) error
	excludeTree = func(id string) error {
		excluded[id] = true
		next := "https://graph.microsoft.com/v1.0/me/mailFolders/" + url.PathEscape(id) + "/childFolders?$top=100&includeHiddenFolders=true"
		for next != "" {
			b, err := graphGet(ctx, token, next)
			if err != nil {
				return err
			}
			var d struct {
				Value []struct {
					ID string `json:"id"`
				} `json:"value"`
				Next string `json:"@odata.nextLink"`
			}
			if err = json.Unmarshal(b, &d); err != nil {
				return err
			}
			for _, v := range d.Value {
				if !excluded[v.ID] {
					if err = excludeTree(v.ID); err != nil {
						return err
					}
				}
			}
			next = d.Next
		}
		return nil
	}
	for _, name := range []string{"sentitems", "drafts", "deleteditems"} {
		b, err := graphGet(ctx, token, "https://graph.microsoft.com/v1.0/me/mailFolders/"+name+"?$select=id")
		if err != nil {
			return err
		}
		var d struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(b, &d); err != nil {
			return err
		}
		if err = excludeTree(d.ID); err != nil {
			return err
		}
	}
	query := url.Values{"$top": {"100"}, "$select": {"id,parentFolderId,isDraft,receivedDateTime"}, "$filter": {"receivedDateTime ge " + a.Since.UTC().Format(time.RFC3339)}}
	next := "https://graph.microsoft.com/v1.0/me/messages?" + query.Encode()
	for next != "" {
		data, err := graphGet(ctx, token, next)
		if err != nil {
			return err
		}
		var page struct {
			Value []struct {
				ID       string    `json:"id"`
				Folder   string    `json:"parentFolderId"`
				Draft    bool      `json:"isDraft"`
				Received time.Time `json:"receivedDateTime"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if err = json.Unmarshal(data, &page); err != nil {
			return err
		}
		for _, m := range page.Value {
			if m.Draft || excluded[m.Folder] {
				continue
			}
			ref := "graph:" + m.ID
			known, err := s.known(ctx, a, ref, run, false)
			if err != nil {
				return err
			}
			if known {
				continue
			}
			raw, err := graphGet(ctx, token, "https://graph.microsoft.com/v1.0/me/messages/"+url.PathEscape(m.ID)+"/$value")
			if err != nil {
				return err
			}
			if err = s.ingest(ctx, a, ref, "", run, raw, m.Received, false, first); err != nil {
				return err
			}
		}
		next = page.Next
	}
	return nil
}
