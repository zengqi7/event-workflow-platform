package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebhookExecutor struct {
	client *http.Client
}

func NewWebhookExecutor(timeout time.Duration) *WebhookExecutor {
	return &WebhookExecutor{
		client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (e *WebhookExecutor) Execute(ctx context.Context, method, url string, body map[string]any) error {
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
