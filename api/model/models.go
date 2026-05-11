package model

import (
	"time"

	"github.com/google/uuid"
)

// ==================== 数据模型 ====================

type Account struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	APIKey    string    `json:"api_key"`
	IsAdmin   bool      `json:"is_admin"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Domain struct {
	ID                    int        `json:"id"`
	Domain                string     `json:"domain"`
	Hostname              string     `json:"hostname"`
	HostnameID            *int       `json:"hostname_id,omitempty"`
	IsActive              bool       `json:"is_active"`
	Status                string     `json:"status"` // active | pending | disabled
	SubdomainEnabled      bool       `json:"subdomain_enabled"`
	SubdomainRandomLength int        `json:"subdomain_random_length"`
	CreatedAt             time.Time  `json:"created_at"`
	MxCheckedAt           *time.Time `json:"mx_checked_at,omitempty"`
}

type Hostname struct {
	ID          int       `json:"id"`
	Hostname    string    `json:"hostname"`
	IsActive    bool      `json:"is_active"`
	DomainCount int       `json:"domain_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type DomainSummary struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Pending  int `json:"pending"`
	Disabled int `json:"disabled"`
}

type Stats struct {
	TotalMailboxes  int `json:"total_mailboxes"`
	ActiveMailboxes int `json:"active_mailboxes"`
	TotalEmails     int `json:"total_emails"`
	ActiveDomains   int `json:"active_domains"`
	PendingDomains  int `json:"pending_domains"`
	TotalAccounts   int `json:"total_accounts"`
}

type Mailbox struct {
	ID               uuid.UUID `json:"id"`
	AccountID        uuid.UUID `json:"account_id"`
	Address          string    `json:"address"`
	DomainID         int       `json:"domain_id"`
	FullAddress      string    `json:"full_address"`
	IsFavorite       bool      `json:"is_favorite"`
	TGForwardEnabled bool      `json:"tg_forward_enabled"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type Email struct {
	ID          uuid.UUID    `json:"id"`
	MailboxID   uuid.UUID    `json:"mailbox_id"`
	Sender      string       `json:"sender"`
	Subject     string       `json:"subject"`
	BodyText    string       `json:"body_text"`
	BodyHTML    string       `json:"body_html"`
	RawMessage  string       `json:"raw_message,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	SizeBytes   int          `json:"size_bytes"`
	ReceivedAt  time.Time    `json:"received_at"`
}

type Attachment struct {
	ID          int    `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int    `json:"size_bytes"`
	Inline      bool   `json:"inline"`
}

// ==================== 请求/响应 ====================

type CreateAccountReq struct {
	Username string `json:"username" binding:"required,min=2,max=64"`
}

type CreateAccountResp struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	APIKey   string    `json:"api_key"`
}

type AddDomainReq struct {
	Domain string `json:"domain" binding:"required,fqdn"`
}

type DNSInstruction struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
}

type AddDomainResp struct {
	Domain       Domain           `json:"domain"`
	DNSRecords   []DNSInstruction `json:"dns_records"`
	Instructions string           `json:"instructions"`
}

type CreateMailboxReq struct {
	Address string `json:"address,omitempty"` // 可选，为空则随机生成
}

type CreateMailboxResp struct {
	Mailbox Mailbox `json:"mailbox"`
}

type ListResp[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
}

type EmailSummary struct {
	ID         uuid.UUID `json:"id"`
	Sender     string    `json:"sender"`
	Subject    string    `json:"subject"`
	SizeBytes  int       `json:"size_bytes"`
	ReceivedAt time.Time `json:"received_at"`
}

type LatestOTPResponse struct {
	MailboxID   uuid.UUID `json:"mailbox_id"`
	FullAddress string    `json:"full_address"`
	EmailID     uuid.UUID `json:"email_id"`
	Code        string    `json:"code"`
	Subject     string    `json:"subject"`
	Sender      string    `json:"sender"`
	ReceivedAt  time.Time `json:"received_at"`
}
