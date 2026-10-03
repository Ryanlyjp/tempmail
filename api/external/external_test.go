package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"
)

func TestRecipientIsolation(t *testing.T) {
	cases := []struct{ header, want string }{
		{"To: a@example.org\r\nDelivered-To: mother@gmail.com\r\n", "a@example.org"},
		{"To: mother@gmail.com\r\nX-Original-To: a@example.org\r\n", "a@example.org"},
		{"To: a@example.org, b@example.org\r\n", ""},
		{"To: a@example.org\r\nX-Original-To: b@example.org\r\n", ""},
		{"To: a@example.org\r\nCc: b@example.org\r\n", ""},
		{"To: mother@gmail.com\r\n", "mother@gmail.com"},
		{"Original-Recipient: rfc822; a@example.org\r\nDelivered-To: mother@gmail.com\r\n", "a@example.org"},
		{"To: broken <\r\nDelivered-To: mother@gmail.com\r\n", ""},
		{"Subject: a@example.org\r\n", ""},
	}
	for _, c := range cases {
		if got := originalRecipient([]byte(c.header+"\r\nForwarded to b@example.org"), "mother@gmail.com"); got != c.want {
			t.Errorf("%q: got %q want %q", c.header, got, c.want)
		}
	}
}
func TestParsingCharsetAndFolders(t *testing.T) {
	raw := []byte("From: sender@example.org\r\nTo: a@example.org\r\nSubject: =?UTF-8?B?6aqM6K+B56CB?=\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nCaf=E9 verification code: 123456")
	e, err := parseEmail(raw, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "验证码" || !strings.Contains(e.BodyText, "Café") {
		t.Fatalf("charset decode: %+v", e)
	}
	for _, name := range []string{"Sent Items", "Drafts", "Trash", "[Gmail]/Sent Mail", "Archive"} {
		attrs := []string{}
		want := name != "Archive"
		if name == "[Gmail]/Sent Mail" {
			attrs = []string{`\Sent`}
		}
		if excludedFolder(name, attrs) != want {
			t.Errorf("folder %s", name)
		}
	}
	key, err := newKey("alice.test+tag@example.org")
	if err != nil || !strings.HasPrefix(key, "alice.test+tag_") || strings.Contains(key, "@") {
		t.Fatal("key must preserve full local part", err)
	}
}

func TestExternalIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN is not set")
	}
	ctx := context.Background()
	settings, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s, err := New(ctx, dsn, filepath.Join(t.TempDir(), "mail.key"), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.save(ctx, Account{Address: "mother@gmail.com", Provider: "gmail", Enabled: true, Credentials: Credentials{Password: "not-a-real-password"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Exec(ctx, `DELETE FROM external_accounts WHERE id=$1`, a.ID)
	a, err = s.account(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err = s.db.QueryRow(ctx, `SELECT credentials FROM external_accounts WHERE id=$1`, a.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "not-a-real-password") {
		t.Fatal("credentials stored without encryption")
	}
	if _, err = s.unseal(uuid.NewString(), encrypted); err == nil {
		t.Fatal("credentials must be bound to account")
	}
	run := uuid.NewString()
	now := time.Now().UTC()
	raw := func(to, code string) []byte {
		return []byte(fmt.Sprintf("From: sender@example.org\r\nTo: %s\r\nDelivered-To: mother@gmail.com\r\nSubject: verification code: %s\r\nContent-Type: multipart/mixed; boundary=piece\r\n\r\n--piece\r\nContent-Type: text/plain\r\n\r\nYour verification code: %s\r\n--piece\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=proof.txt\r\n\r\nSECRET-%s\r\n--piece--\r\n", to, code, code, code))
	}
	for i, addr := range []string{"a@example.org", "b@example.org"} {
		if err = s.ingest(ctx, a, fmt.Sprint(i), "", run, raw(addr, fmt.Sprint(123456+i)), now, false, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.ingest(ctx, a, "other-folder", "", run, raw("a@example.org", "123456"), now, false, false); err != nil {
		t.Fatal(err)
	}
	if err = s.ingest(ctx, a, "ambiguous", "", run, raw("a@example.org, b@example.org", "888888"), now, false, false); err != nil {
		t.Fatal(err)
	}
	var count int
	s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1`, a.ID).Scan(&count)
	if count != 3 {
		t.Fatalf("duplicate messages: %d", count)
	}
	var aID, bID, mailboxID string
	s.db.QueryRow(ctx, `SELECT id FROM external_messages WHERE account_id=$1 AND recipient='a@example.org'`, a.ID).Scan(&aID)
	s.db.QueryRow(ctx, `SELECT id FROM external_messages WHERE account_id=$1 AND recipient='b@example.org'`, a.ID).Scan(&bID)
	s.db.QueryRow(ctx, `SELECT id FROM external_mailboxes WHERE account_id=$1 AND address='a@example.org'`, a.ID).Scan(&mailboxID)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api/admin")
	admin.Use(func(c *gin.Context) {
		c.Set(middleware.AccountKey, &model.Account{IsAdmin: c.GetHeader("Authorization") == "Bearer admin-test"})
	})
	admin.Use(middleware.AdminOnly())
	s.Register(admin, r.Group("/public"))
	request := func(method, path, key, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	check := func(method, path, key, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := request(method, path, key, body)
		if w.Code != status {
			t.Fatalf("%s %s status %d expected %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	check("GET", "/api/admin/external/accounts", "normal-key", "", 403)
	list := check("GET", "/api/admin/external/accounts", "admin-test", "", 200)
	if strings.Contains(list.Body.String(), "not-a-real-password") {
		t.Fatal("credential leak")
	}
	endpoint := "/api/admin/external/mailboxes/" + mailboxID + "/share"
	w := check("PUT", endpoint, "admin-test", `{"enabled":true,"expires_days":1}`, 200)
	var payload struct {
		Share Share `json:"share"`
	}
	json.Unmarshal(w.Body.Bytes(), &payload)
	key := payload.Share.APIKey
	check("GET", "/public/external-otp/latest", "", "", 401)
	check("GET", "/public/external-otp/latest", "admin-test", "", 401)
	for _, base := range []string{"/public/external-otp", "/public/external-otp/page/" + key} {
		w = check("GET", base+"/latest?format=text&mailbox_id="+uuid.NewString()+"&address=b@example.org", key, "", 200)
		if strings.TrimSpace(w.Body.String()) != "123456" {
			t.Fatal("wrong OTP")
		}
		w = check("GET", base+"/emails", key, "", 200)
		if strings.Contains(w.Body.String(), bID) || strings.Contains(w.Body.String(), "888888") {
			t.Fatal("list isolation failed")
		}
		check("GET", base+"/emails/"+aID, key, "", 200)
		for _, suffix := range []string{"", "/otp", "/attachments/1"} {
			check("GET", base+"/emails/"+bID+suffix, key, "", 404)
		}
		check("GET", base+"/emails/"+aID+"/attachments/1", key, "", 200)
	}
	check("GET", "/api/admin/external/mailboxes/"+mailboxID+"/otp/latest?format=text", "admin-test", "", 200)
	check("GET", "/api/admin/external/accounts/"+a.ID+"/otp/latest?format=text", "admin-test", "", 200)
	check("PUT", "/api/admin/external/accounts/"+a.ID+"/tg", "admin-test", `{"tg_enabled":true}`, 200)
	var accountTG bool
	if err = s.db.QueryRow(ctx, `SELECT tg_enabled FROM external_accounts WHERE id=$1`, a.ID).Scan(&accountTG); err != nil || !accountTG {
		t.Fatal("account TG setting not enabled", err)
	}
	check("PUT", "/api/admin/external/accounts/"+a.ID+"/tg", "admin-test", `{"tg_enabled":false}`, 200)
	check("PUT", endpoint, "admin-test", `{"enabled":false}`, 200)
	check("GET", "/public/external-otp/latest", key, "", 401)
	check("PUT", endpoint, "admin-test", `{"enabled":true}`, 200)
	s.db.Exec(ctx, `UPDATE external_shares SET expires_at=NOW()-INTERVAL '1 second' WHERE mailbox_id=$1`, mailboxID)
	check("GET", "/public/external-otp/latest", key, "", 401)
	w = check("PUT", endpoint, "admin-test", `{"enabled":true,"rotate":true,"expires_days":0}`, 200)
	json.Unmarshal(w.Body.Bytes(), &payload)
	check("GET", "/public/external-otp/latest", key, "", 401)
	check("GET", "/public/external-otp/latest", payload.Share.APIKey, "", 200)
	check("DELETE", endpoint, "admin-test", "", 200)
	check("GET", "/public/external-otp/latest", payload.Share.APIKey, "", 401)
	// With no Bot configured, any attempted notification would fail. Promotions
	// and initial-import messages must still finish without invoking the sender.
	s.db.Exec(ctx, `UPDATE external_mailboxes SET tg_enabled=true WHERE id=$1`, mailboxID)
	if err = s.ingest(ctx, a, "promotion", "", run, raw("a@example.org", "777777"), now, true, false); err != nil {
		t.Fatal(err)
	}
	if err = s.notify(ctx, a); err != nil {
		t.Fatal("history/promotion was notified", err)
	}
	var pending int
	s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1 AND NOT notified`, a.ID).Scan(&pending)
	if pending != 0 {
		t.Fatal("notification markers not completed")
	}

	// Removing a child deletes its share but retains the mother account's mail.
	w = check("PUT", endpoint, "admin-test", `{"enabled":true,"expires_days":0}`, 200)
	json.Unmarshal(w.Body.Bytes(), &payload)
	childKey := payload.Share.APIKey
	check("DELETE", "/api/admin/external/mailboxes/"+mailboxID, "normal-key", "", 403)
	check("DELETE", "/api/admin/external/mailboxes/"+mailboxID, "admin-test", "", 200)
	check("DELETE", "/api/admin/external/mailboxes/"+mailboxID, "admin-test", "", 404)
	check("GET", "/public/external-otp/latest", childKey, "", 401)
	if err = s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1`, a.ID).Scan(&count); err != nil || count != 4 {
		t.Fatal("child deletion changed synchronized messages", err, count)
	}
	check("GET", "/api/admin/external/accounts/"+a.ID+"/emails", "admin-test", "", 200)
	if err = s.ingest(ctx, a, "rediscover", "", run, raw("a@example.org", "999999"), now.Add(time.Second), false, false); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(ctx, `SELECT count(*) FROM external_mailboxes WHERE account_id=$1 AND address='a@example.org'`, a.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("new mail did not rediscover deleted child", err, count)
	}

	deleteAccount, err := s.save(ctx, Account{Address: "delete-fixture@gmail.com", Provider: "gmail", Enabled: true, Credentials: Credentials{Password: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ensureMailbox(ctx, deleteAccount.ID, "delete-child@example.org"); err != nil {
		t.Fatal(err)
	}
	var deleteMailboxID string
	if err = s.db.QueryRow(ctx, `SELECT id FROM external_mailboxes WHERE account_id=$1`, deleteAccount.ID).Scan(&deleteMailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, `INSERT INTO external_shares(mailbox_id,api_key) VALUES($1,$2)`, deleteMailboxID, "delete-fixture-key"); err != nil {
		t.Fatal(err)
	}
	check("DELETE", "/api/admin/external/accounts/"+deleteAccount.ID, "normal-key", "", 403)
	check("DELETE", "/api/admin/external/accounts/"+deleteAccount.ID, "admin-test", "", 200)
	if err = s.db.QueryRow(ctx, `SELECT count(*) FROM external_mailboxes WHERE account_id=$1`, deleteAccount.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("account deletion did not cascade", err, count)
	}
	check("DELETE", "/api/admin/external/accounts/"+deleteAccount.ID, "admin-test", "", 404)
}
