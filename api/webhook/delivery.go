package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type Delivery struct {
	ID       string
	Payload  []byte
	Attempts int
}

type OutboxStore interface {
	GetSetting(context.Context, string) (string, error)
	NextWebhookDelivery(context.Context) (*Delivery, error)
	CompleteWebhookDelivery(context.Context, string) error
	RetryWebhookDelivery(context.Context, string, int, time.Time, string) error
	DeleteExpiredWebhookDeliveries(context.Context, time.Time) (int64, error)
}

type Worker struct {
	store  OutboxStore
	client *http.Client
}

func NewWorker(store OutboxStore) *Worker {
	return &Worker{
		store: store,
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func retryDelay(attempts int) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}
	if attempts < len(delays) {
		return delays[attempts]
	}
	return delays[len(delays)-1]
}

func (w *Worker) Run(ctx context.Context, logf func(string, ...any)) {
	ticker := time.NewTicker(2 * time.Second)
	cleanup := time.NewTicker(time.Hour)
	defer ticker.Stop()
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.deliverOne(ctx, logf)
		case <-cleanup.C:
			deleted, err := w.store.DeleteExpiredWebhookDeliveries(ctx, time.Now().Add(-24*time.Hour))
			if err != nil {
				logf("[webhook] outbox cleanup failed: %v", err)
			} else if deleted > 0 {
				logf("[webhook] removed %d deliveries older than 24 hours", deleted)
			}
		}
	}
}

func (w *Worker) deliverOne(ctx context.Context, logf func(string, ...any)) {
	cfg, err := Load(ctx, w.store)
	if err != nil || !cfg.Enabled {
		return
	}
	delivery, err := w.store.NextWebhookDelivery(ctx)
	if err != nil || delivery == nil {
		if err != nil {
			logf("[webhook] read outbox failed: %v", err)
		}
		return
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(delivery.Payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TempMail-Delivery", delivery.ID)
		req.Header.Set("X-TempMail-Timestamp", timestamp)
		req.Header.Set("X-TempMail-Signature", Signature(cfg.Secret, timestamp, delivery.Payload))
		var response *http.Response
		response, err = w.client.Do(req)
		if response != nil {
			response.Body.Close()
			if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				if completeErr := w.store.CompleteWebhookDelivery(ctx, delivery.ID); completeErr != nil {
					logf("[webhook] mark delivery %s complete failed: %v", delivery.ID, completeErr)
				}
				return
			}
			if err == nil {
				err = fmt.Errorf("HTTP %d", response.StatusCode)
			}
		}
	}
	if err == nil {
		err = fmt.Errorf("unknown delivery error")
	}
	attempts := delivery.Attempts + 1
	if retryErr := w.store.RetryWebhookDelivery(ctx, delivery.ID, attempts, time.Now().Add(retryDelay(delivery.Attempts)), err.Error()); retryErr != nil {
		logf("[webhook] schedule retry %s failed: %v", delivery.ID, retryErr)
	}
}
