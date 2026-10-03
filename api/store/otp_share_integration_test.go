package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMailboxOTPShareIsolationAndState(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN is not set")
	}

	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	suffix := uuid.NewString()
	accountID := uuid.New()
	mailboxA := uuid.New()
	mailboxB := uuid.New()
	domain := "share-test-" + suffix + ".example"
	var domainID int
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO accounts (id, username, api_key) VALUES ($1, $2, $3)`,
		accountID, "share-test-"+suffix, "tm_test_"+suffix,
	); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO domains (domain) VALUES ($1) RETURNING id`, domain,
	).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.pool.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID)
		s.pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, domainID)
	}()

	for _, item := range []struct {
		id      uuid.UUID
		address string
	}{
		{mailboxA, "first"},
		{mailboxB, "second"},
	} {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO mailboxes (id, account_id, address, domain_id, full_address, is_favorite)
			 VALUES ($1, $2, $3, $4, $5, TRUE)`,
			item.id, accountID, item.address, domainID, fmt.Sprintf("%s@%s", item.address, domain),
		); err != nil {
			t.Fatal(err)
		}
	}

	var otherEmailID uuid.UUID
	for i := 0; i < 6; i++ {
		if _, err := s.InsertEmail(ctx, mailboxA, "sender@example.com", fmt.Sprintf("A-%d", i), "code 123456", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	otherEmail, err := s.InsertEmail(ctx, mailboxB, "sender@example.com", "B", "code 654321", "", "")
	if err != nil {
		t.Fatal(err)
	}
	otherEmailID = otherEmail.ID

	shareA, err := s.UpsertMailboxOTPShare(ctx, mailboxA, accountID, "share_api_key_first_1234", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertMailboxOTPShare(ctx, mailboxB, accountID, "share_api_key_second_123", true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE mailbox_otp_shares SET token = $1 WHERE mailbox_id = $2`,
		"legacy_page_token_first", mailboxA,
	); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureSchemaCompat(ctx); err != nil {
		t.Fatal(err)
	}
	var mirroredToken string
	if err := s.pool.QueryRow(ctx,
		`SELECT token FROM mailbox_otp_shares WHERE mailbox_id = $1`, mailboxA,
	).Scan(&mirroredToken); err != nil || mirroredToken != shareA.APIKey {
		t.Fatalf("migrated page credential = %q, want %q, err=%v", mirroredToken, shareA.APIKey, err)
	}
	resolved, err := s.GetMailboxOTPShareByAPIKey(ctx, shareA.APIKey)
	if err != nil || resolved.MailboxID != mailboxA {
		t.Fatalf("api key resolved mailbox %v, %v", resolved, err)
	}
	if _, err := s.GetEmail(ctx, otherEmailID, resolved.MailboxID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("share accessed another mailbox email: %v", err)
	}

	emails, total, err := s.ListEmails(ctx, resolved.MailboxID, 1, 5)
	if err != nil || len(emails) != 5 || total != 6 {
		t.Fatalf("recent emails len=%d total=%d err=%v", len(emails), total, err)
	}

	if _, err := s.UpsertMailboxOTPShare(ctx, mailboxA, accountID, shareA.APIKey, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMailboxOTPShareByAPIKey(ctx, shareA.APIKey); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stopped share remained accessible: %v", err)
	}

	expiredAt := time.Now().Add(-time.Minute)
	if _, err := s.UpsertMailboxOTPShare(ctx, mailboxA, accountID, shareA.APIKey, true, &expiredAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMailboxOTPShareByAPIKey(ctx, shareA.APIKey); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expired share remained accessible: %v", err)
	}
	generated, err := s.UpsertMailboxOTPShare(ctx, mailboxA, accountID, "", true, nil)
	if err != nil || !strings.HasPrefix(generated.APIKey, "first_") {
		t.Fatalf("generated key does not include mailbox name: %v", err)
	}
	longName := strings.Repeat("a", 64)
	if _, err := s.pool.Exec(ctx, `UPDATE mailboxes SET address=$2,full_address=$3 WHERE id=$1`, mailboxB, longName, longName+"@"+domain); err != nil {
		t.Fatal(err)
	}
	longShare, err := s.UpsertMailboxOTPShare(ctx, mailboxB, accountID, "", true, nil)
	if err != nil || !strings.HasPrefix(longShare.APIKey, longName+"_") {
		t.Fatalf("long mailbox name key failed: %v", err)
	}
}
