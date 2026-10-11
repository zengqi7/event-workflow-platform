package notifier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fixedSecret []byte

func (s fixedSecret) Resolve(_ context.Context, tenant uint64, ref string) ([]byte, error) {
	return []byte(s), nil
}

func TestSignedDeliveryAndRedirectRefusal(t *testing.T) {
	secret := fixedSecret("01234567890123456789012345678901")
	var received int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", 302)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(r.Header.Get("X-Webhook-Timestamp") + "."))
		mac.Write(raw)
		if r.Header.Get("X-Webhook-Signature") != "v1="+hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("Idempotency-Key") != "stable" {
			t.Error("signature or idempotency mismatch")
		}
	}))
	defer server.Close()
	executor := NewWithPolicy(time.Second, Policy{Development: true, AllowedHosts: []string{"127.0.0.1"}}, secret)
	body := map[string]any{"idempotency_key": "stable", "value": 1}
	for range 2 {
		if err := executor.ExecuteDelivery(context.Background(), "POST", server.URL, body, 1, "billing"); err != nil {
			t.Fatal(err)
		}
	}
	if err := executor.ExecuteDelivery(context.Background(), "POST", server.URL+"/redirect", body, 1, "billing"); err == nil {
		t.Fatal("redirect accepted")
	}
	if received != 3 {
		t.Fatal("redirect followed", received)
	}
	if err := NewWebhookExecutor(time.Second).Execute(context.Background(), "POST", server.URL, body); err == nil {
		t.Fatal("default constructor permits network")
	}
	if err := NewWithPolicy(time.Second, Policy{AllowedHosts: []string{"127.0.0.1"}}, secret).ExecuteDelivery(context.Background(), "POST", "https://127.0.0.1", body, 1, "billing"); err == nil {
		t.Fatal("private address accepted")
	}
}

func TestProductionPolicyAndAddressClassification(t *testing.T) {
	p := Policy{AllowedHosts: []string{"hooks.example.com"}}
	for _, raw := range []string{"http://hooks.example.com", "https://hooks.example.com:8443", "https://user@hooks.example.com", "https://hooks.example.com/#x", "https://hooks.example.com.attacker.net"} {
		if p.ValidateURL(raw) == nil {
			t.Error("allowed", raw)
		}
	}
	if err := p.ValidateURL("https://hooks.example.com/path"); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "2001:db8::1"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Error("allowed", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(ip)) {
			t.Error("blocked public", ip)
		}
	}
}

func TestTenantSecretConfinement(t *testing.T) {
	root := t.TempDir()
	for _, tenant := range []string{"1", "2"} {
		if err := os.Mkdir(filepath.Join(root, tenant), 0700); err != nil {
			t.Fatal(err)
		}
	}
	key := []byte("01234567890123456789012345678901")
	if err := os.WriteFile(filepath.Join(root, "2", "key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	resolver := DirectorySecrets{Root: root}
	if _, err := resolver.Resolve(context.Background(), 1, "key"); err == nil {
		t.Fatal("cross tenant key available")
	}
	if _, err := resolver.Resolve(context.Background(), 1, "../2/key"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := resolver.Resolve(context.Background(), 2, "key"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "2", "key"), filepath.Join(root, "1", "escape")); err != nil {
		t.Log("symlink privilege unavailable; Linux CI covers confinement")
		return
	}
	if _, err := resolver.Resolve(context.Background(), 1, "escape"); err == nil {
		t.Fatal("escaping symlink accepted")
	}
}
