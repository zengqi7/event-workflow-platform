CREATE TABLE api_keys (
 id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
 tenant_id BIGINT UNSIGNED NOT NULL,
 token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 subject VARCHAR(128) NOT NULL,
 scopes_json JSON NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 revoked_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_api_key_hash(token_hash),
 CONSTRAINT fk_api_key_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
ALTER TABLE tenants ADD COLUMN requests_per_minute INT UNSIGNED NOT NULL DEFAULT 600, ADD COLUMN max_pending_tasks INT UNSIGNED NOT NULL DEFAULT 10000;
CREATE TABLE tenant_request_windows (
 tenant_id BIGINT UNSIGNED PRIMARY KEY,
 window_minute BIGINT NOT NULL,
 request_count INT UNSIGNED NOT NULL,
 CONSTRAINT fk_window_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
CREATE TABLE audit_events (
 id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
 tenant_id BIGINT UNSIGNED NOT NULL,
 key_id BIGINT UNSIGNED NULL,
 subject VARCHAR(128) NOT NULL,
 action VARCHAR(128) NOT NULL,
 resource VARCHAR(255) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 KEY idx_audit_tenant_time(tenant_id,created_at,id),
 CONSTRAINT fk_audit_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB;
