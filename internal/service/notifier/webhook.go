package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type WebhookExecutor struct {
	client  *http.Client
	policy  Policy
	secrets SecretResolver
}

func NewWebhookExecutor(timeout time.Duration) *WebhookExecutor {
	return NewWithPolicy(timeout, Policy{}, nil)
}

func (e *WebhookExecutor) Execute(ctx context.Context, method, url string, body map[string]any) error {
	return e.ExecuteDelivery(ctx, method, url, body, 0, "")
}

func (e *WebhookExecutor) ExecuteDelivery(ctx context.Context, method, url string, body map[string]any, tenantID uint64, ref string) error {
	if err := e.policy.ValidateURL(url); err != nil {
		return err
	}
	if method != "POST" && method != "PUT" {
		return fmt.Errorf("unsupported method")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key, ok := body["idempotency_key"].(string); ok {
		req.Header.Set("Idempotency-Key", key)
	}
	if ref != "" {
		if e.secrets == nil {
			return fmt.Errorf("secret resolver unavailable")
		}
		secret, err := e.secrets.Resolve(ctx, tenantID, ref)
		if err != nil {
			return fmt.Errorf("signing secret unavailable")
		}
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(stamp + "."))
		mac.Write(payload)
		req.Header.Set("X-Webhook-Timestamp", stamp)
		req.Header.Set("X-Webhook-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
	} else if !e.policy.Development {
		return fmt.Errorf("secret_ref required for production delivery")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
