CREATE TABLE event_dedup_claims (
  tenant_id BIGINT UNSIGNED NOT NULL,
  dedup_key VARCHAR(128) NOT NULL,
  dedup_bucket VARCHAR(32) NOT NULL,
  event_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (tenant_id, dedup_key, dedup_bucket),
  CONSTRAINT fk_claim_event FOREIGN KEY (event_id) REFERENCES events(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

INSERT INTO event_dedup_claims (tenant_id, dedup_key, dedup_bucket, event_id)
SELECT tenant_id, dedup_key, dedup_bucket, MIN(id) FROM events WHERE status = 'received' GROUP BY tenant_id, dedup_key, dedup_bucket;

ALTER TABLE outbox_events ADD COLUMN lease_token VARCHAR(64) NULL, ADD COLUMN lease_until DATETIME(3) NULL, ADD KEY idx_outbox_lease (status, lease_until);
UPDATE outbox_events SET status = 'pending' WHERE status = 'processing';
ALTER TABLE events MODIFY source VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL, MODIFY event_id VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL;
