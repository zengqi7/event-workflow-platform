ALTER TABLE tenants
 ADD COLUMN max_concurrent_tasks INT NOT NULL DEFAULT 8,
 ADD COLUMN dispatches_per_minute INT NOT NULL DEFAULT 600,
 ADD COLUMN dispatch_window VARCHAR(16) NULL,
 ADD COLUMN dispatch_count INT NOT NULL DEFAULT 0;
ALTER TABLE workflow_tasks ADD COLUMN attempt_count INT NOT NULL DEFAULT 0;
UPDATE workflow_tasks SET attempt_count=retry_count+IF(status IN ('running','success'),1,0);
CREATE TABLE scheduler_state (
 id TINYINT PRIMARY KEY,
 last_tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 0
) ENGINE=InnoDB;
INSERT INTO scheduler_state(id,last_tenant_id) VALUES(1,0);
ALTER TABLE outbox_events ADD KEY idx_outbox_tenant_ready(tenant_id,status,next_retry_at,id);
