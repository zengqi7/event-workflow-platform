package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

func Fingerprint(tenantID uint64, source, eventType, eventID, businessKey string) string {
	raw := fmt.Sprintf("%d|%s|%s|%s|%s", tenantID, source, eventType, eventID, businessKey)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

func DedupKey(tenantID uint64, eventType, businessKey string) string {
	raw := fmt.Sprintf("%d|%s|%s", tenantID, eventType, businessKey)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

func DedupBucket(occurredAt time.Time) string {
	return occurredAt.UTC().Format("200601021504")
}
