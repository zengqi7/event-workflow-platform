package mysqlrepo

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) QueueMetrics(ctx context.Context) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM outbox_events GROUP BY status`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var output strings.Builder
	output.WriteString("# TYPE outbox_queue_depth gauge\n")
	allowed := map[string]bool{"pending": true, "blocked": true, "processing": true, "published": true, "failed": true, "skipped": true}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return "", err
		}
		if allowed[state] {
			fmt.Fprintf(&output, "outbox_queue_depth{state=%q} %d\n", state, count)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	var expired int
	var age float64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status='processing' AND lease_until<=UTC_TIMESTAMP(3)`).Scan(&expired); err != nil {
		return "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(TIMESTAMPDIFF(MICROSECOND,created_at,UTC_TIMESTAMP(3)))/1000000,0) FROM outbox_events WHERE status='pending' AND (next_retry_at IS NULL OR next_retry_at<=UTC_TIMESTAMP(3))`).Scan(&age); err != nil {
		return "", err
	}
	fmt.Fprintf(&output, "# TYPE outbox_expired_leases gauge\noutbox_expired_leases %d\n# TYPE outbox_ready_oldest_age_seconds gauge\noutbox_ready_oldest_age_seconds %.3f\n", expired, age)
	return output.String(), nil
}
