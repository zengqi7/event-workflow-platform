package auth

import (
	"testing"
	"time"
)

func TestKeys(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(Hash(first)) != 64 || Hash(first) == first {
		t.Fatal("invalid credential entropy/hash")
	}
	for _, scopes := range [][]string{nil, {"*"}, {"state:read", "state:read"}} {
		if ValidateKey("ci", scopes, time.Now().Add(time.Hour)) == nil {
			t.Fatal("accepted invalid scopes")
		}
	}
	if ValidateKey("ci", []string{"state:read"}, time.Now().Add(-time.Second)) == nil {
		t.Fatal("accepted expiry")
	}
}
