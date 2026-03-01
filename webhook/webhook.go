package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Event is the payload sent to webhook endpoints.
type Event struct {
	Type      string      `json:"type"`      // e.g. "batch.completed", "crawl.page", "crawl.completed", "crawl.failed"
	JobID     string      `json:"job_id"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Deliver sends a webhook event synchronously.
// The request body is signed with HMAC-SHA256 if secret is non-empty.
// Header: X-Purify-Signature: sha256=<hex>
func Deliver(ctx context.Context, url, secret string, event *Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook: marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Purify-Webhook/1.0")

	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Purify-Signature", "sha256="+sig)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: deliver: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// DeliverAsync sends a webhook event asynchronously with up to 3 retries.
// Retry intervals: 1s, 5s, 30s.
func DeliverAsync(url, secret string, event *Event) {
	go func() {
		delays := []time.Duration{0, 1 * time.Second, 5 * time.Second, 30 * time.Second}
		for attempt, delay := range delays {
			if delay > 0 {
				time.Sleep(delay)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := Deliver(ctx, url, secret, event)
			cancel()
			if err == nil {
				slog.Info("webhook delivered",
					"url", url,
					"event", event.Type,
					"job_id", event.JobID,
					"attempt", attempt+1,
				)
				return
			}
			slog.Warn("webhook delivery failed",
				"url", url,
				"event", event.Type,
				"job_id", event.JobID,
				"attempt", attempt+1,
				"error", err,
			)
		}
		slog.Error("webhook delivery exhausted all retries",
			"url", url,
			"event", event.Type,
			"job_id", event.JobID,
		)
	}()
}
