package external

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/client"
	"github.com/google/uuid"
	"tempmail/store"
)

func TestGmailCategorySearchCommand(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	observed := make(chan string, 1)
	go func() {
		fmt.Fprint(right, "* OK [CAPABILITY IMAP4rev1 X-GM-EXT-1] ready\r\n")
		r := bufio.NewReader(right)
		line, _ := r.ReadString('\n')
		observed <- line
		tag := strings.Fields(line)[0]
		fmt.Fprintf(right, "* SEARCH 2 9\r\n%s OK search complete\r\n", tag)
	}()
	c, err := client.New(left)
	if err != nil {
		t.Fatal(err)
	}
	c.Timeout = time.Second
	ids, err := gmailSearch(c, "category:promotions after:123456")
	if err != nil {
		t.Fatal(err)
	}
	if !ids[2] || !ids[9] || len(ids) != 2 {
		t.Fatal(ids)
	}
	if !strings.Contains(<-observed, `UID SEARCH X-GM-RAW "category:promotions after:123456"`) {
		t.Fatal("missing exact Gmail classification query")
	}
}

type testRoundTrip func(*http.Request) (*http.Response, error)

func (f testRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGraphSyncPaginationRotationAndDedup(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	ctx := context.Background()
	settings, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s, err := New(ctx, dsn, filepath.Join(t.TempDir(), "key"), settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.save(ctx, Account{Address: "graph@example.org", Provider: "outlook", Protocol: "graph", Enabled: true, Credentials: Credentials{ClientID: "client", RefreshToken: "old-refresh"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Exec(ctx, `DELETE FROM external_accounts WHERE id=$1`, a.ID)
	a, _ = s.account(ctx, a.ID)
	old := remoteHTTP
	defer func() { remoteHTTP = old }()
	mimeCalls := 0
	now := time.Now().UTC().Format(time.RFC3339)
	remoteHTTP = &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := ""
		path := req.URL.Path
		switch {
		case req.URL.Host == "login.microsoftonline.com":
			body = `{"access_token":"access","refresh_token":"new-refresh"}`
		case strings.HasSuffix(path, "/childFolders"):
			body = `{"value":[]}`
		case strings.Contains(path, "/mailFolders/"):
			name := path[strings.LastIndex(path, "/")+1:]
			body = `{"id":"` + name + `"}`
		case strings.HasSuffix(path, "/$value"):
			mimeCalls++
			body = "From: microsoft@example.org\r\nTo: child@example.org\r\nSubject: Verification code 123456\r\nContent-Type: text/plain\r\n\r\nYour verification code: 123456"
		case req.URL.Query().Get("$skiptoken") == "next":
			body = fmt.Sprintf(`{"value":[{"id":"received-2","parentFolderId":"archive","receivedDateTime":%q}]}`, now)
		default:
			body = fmt.Sprintf(`{"value":[{"id":"received-1","parentFolderId":"inbox","receivedDateTime":%q},{"id":"sent","parentFolderId":"sentitems","receivedDateTime":%q},{"id":"draft","isDraft":true,"receivedDateTime":%q}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/messages?$skiptoken=next"}`, now, now, now)
		}
		if req.URL.Host == "graph.microsoft.com" && (req.Header.Get("Authorization") != "Bearer access" || req.Header.Get("Prefer") != `IdType="ImmutableId"`) {
			t.Error("Graph auth / stable id headers missing")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if err = s.graphSync(ctx, a, uuid.NewString(), true); err != nil {
		t.Fatal(err)
	}
	if err = s.graphSync(ctx, a, uuid.NewString(), false); err != nil {
		t.Fatal(err)
	}
	if mimeCalls != 2 {
		t.Fatalf("incremental sync fetched %d bodies instead of 2", mimeCalls)
	}
	var count int
	s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1`, a.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate MIME content not deduplicated: %d", count)
	}
	updated, err := s.account(ctx, a.ID)
	if err != nil || updated.Credentials.RefreshToken != "new-refresh" {
		t.Fatal("rotated refresh token not persisted", err)
	}
	data, _ := json.Marshal(updated)
	if strings.Contains(string(data), "access") {
		t.Fatal("access token should not be persisted")
	}
}
