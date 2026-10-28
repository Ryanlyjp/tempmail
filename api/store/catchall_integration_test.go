package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestCatchAllMailboxUsesConfiguredAccount(t *testing.T) {
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

	previousAccountID, _ := s.GetSetting(ctx, "catchall_account_id")
	owner, err := s.CreateAccount(ctx, "catchall-owner-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateAccount(ctx, "catchall-other-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	domain, err := s.AddDomain(ctx, "catchall-"+uuid.NewString()+".example", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = s.SetSetting(ctx, "catchall_account_id", previousAccountID)
		_ = s.DeleteAccount(ctx, owner.ID)
		_ = s.DeleteAccount(ctx, other.ID)
		_ = s.DeleteDomain(ctx, domain.ID)
	}()

	if err := s.SetSetting(ctx, "catchall_account_id", owner.ID.String()); err != nil {
		t.Fatal(err)
	}
	ownerID, err := s.GetCatchAllAccountID(ctx)
	if err != nil || ownerID != owner.ID {
		t.Fatalf("catch-all owner = %s, want %s, err=%v", ownerID, owner.ID, err)
	}

	fullAddress := "unknown@" + domain.Domain
	mailbox, err := s.EnsureCatchAllMailbox(ctx, ownerID, "unknown", domain.ID, fullAddress, 30)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.AccountID != owner.ID {
		t.Fatalf("new mailbox account = %s, want %s", mailbox.AccountID, owner.ID)
	}

	existing, err := s.EnsureCatchAllMailbox(ctx, other.ID, "unknown", domain.ID, fullAddress, 30)
	if err != nil {
		t.Fatal(err)
	}
	if existing.ID != mailbox.ID || existing.AccountID != owner.ID {
		t.Fatalf("existing mailbox was reassigned: id=%s account=%s", existing.ID, existing.AccountID)
	}

	if err := s.SetSetting(ctx, "catchall_account_id", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCatchAllAccountID(ctx); err == nil {
		t.Fatal("missing configured account unexpectedly fell back to an administrator")
	}
}
