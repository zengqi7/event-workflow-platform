ALTER TABLE workflow_runs ADD COLUMN replay_of BIGINT UNSIGNED NULL, ADD COLUMN replay_generation INT NOT NULL DEFAULT 0, ADD COLUMN replay_reason VARCHAR(255) NULL, ADD KEY idx_replay_origin(replay_of,replay_generation);
CREATE TABLE run_replay_requests (
 tenant_id BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
 source_run_id BIGINT UNSIGNED NOT NULL,
 new_run_id BIGINT UNSIGNED NOT NULL,
 reason VARCHAR(255) NOT NULL,
 subject VARCHAR(128) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY(tenant_id,request_key),
 CONSTRAINT fk_replay_source FOREIGN KEY(source_run_id) REFERENCES workflow_runs(id),
 CONSTRAINT fk_replay_new FOREIGN KEY(new_run_id) REFERENCES workflow_runs(id)
) ENGINE=InnoDB;
