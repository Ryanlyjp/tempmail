package external

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
	"github.com/google/uuid"
	"tempmail/store"
)

type testIMAPBackend struct{ *memory.Backend }

func (b testIMAPBackend) Login(info *imap.ConnInfo, user, password string) (backend.User, error) {
	return b.Backend.Login(info, "username", password)
}

func TestIMAPReadOnlySync(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	ctx := context.Background()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	caFile := filepath.Join(t.TempDir(), "test-ca.pem")
	if err = os.WriteFile(caFile, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", caFile)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	be := memory.New()
	u, _ := be.Login(nil, "username", "password")
	mb, _ := u.GetMailbox("INBOX")
	inbox := mb.(*memory.Mailbox)
	inbox.Messages[0].Flags = nil
	for _, name := range []string{"Archive", "Sent", "Drafts", "Trash"} {
		u.CreateMailbox(name)
		m, _ := u.GetMailbox(name)
		m.(*memory.Mailbox).Messages = append(m.(*memory.Mailbox).Messages, inbox.Messages[0])
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(testIMAPBackend{be})
	defer srv.Close()
	go srv.Serve(listener)
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
	host, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	a, err := s.save(ctx, Account{Address: "mother@example.org", Provider: "custom", Host: host, Port: port, Enabled: true, Credentials: Credentials{Password: "password"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Exec(ctx, `DELETE FROM external_accounts WHERE id=$1`, a.ID)
	a, _ = s.account(ctx, a.ID)
	for i := 0; i < 2; i++ {
		if err = s.imapSync(ctx, a, uuid.NewString(), i == 0, false); err != nil {
			t.Fatal(err)
		}
	}
	var count, refs int
	s.db.QueryRow(ctx, `SELECT count(*) FROM external_messages WHERE account_id=$1`, a.ID).Scan(&count)
	s.db.QueryRow(ctx, `SELECT count(*) FROM external_sources WHERE account_id=$1`, a.ID).Scan(&refs)
	if count != 1 || refs != 2 {
		t.Fatalf("want one mail in INBOX and Archive, got mails=%d sources=%d", count, refs)
	}
	if len(inbox.Messages[0].Flags) != 0 {
		t.Fatal("sync changed remote message flags")
	}
}
