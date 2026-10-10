package infra

import (
	"crypto/rand"
	"encoding/hex"
)

func NewTraceID() string {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "trace-id-fallback"
	}
	return hex.EncodeToString(buf)
}
