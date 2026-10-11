package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("invalid or expired credential")
var ErrQuota = errors.New("tenant quota exceeded")

type Principal struct {
	KeyID      uint64
	TenantID   uint64
	TenantCode string
	Subject    string
	Scopes     []string
}

func (p Principal) Allows(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

type Repository interface {
	Authenticate(context.Context, string) (Principal, error)
	Admit(context.Context, Principal, string, string) error
}

func Hash(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }
func NewToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "ewp_" + hex.EncodeToString(raw[:]), nil
}
func ValidateKey(subject string, scopes []string, expiry time.Time) error {
	if strings.TrimSpace(subject) == "" || len(subject) > 128 || !expiry.After(time.Now()) || len(scopes) == 0 {
		return errors.New("subject, scope and future expiry required")
	}
	seen := map[string]bool{}
	for _, s := range scopes {
		switch s {
		case "events:write", "state:read", "workflows:write", "runs:replay":
		default:
			return errors.New("unknown scope")
		}
		if seen[s] {
			return errors.New("duplicate scope")
		}
		seen[s] = true
	}
	return nil
}
