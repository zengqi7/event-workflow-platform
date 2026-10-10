CREATE TABLE tenants (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  name VARCHAR(128) NOT NULL,
  code VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_tenants_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE events (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT UNSIGNED NOT NULL,
  source VARCHAR(64) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  event_id VARCHAR(128) NOT NULL,
  business_key VARCHAR(128) NULL,
  trace_id VARCHAR(64) NULL,
  fingerprint VARCHAR(128) NOT NULL,
  dedup_key VARCHAR(128) NOT NULL,
  dedup_bucket VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'received',
  payload_json JSON NOT NULL,
  occurred_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_events_idempotency (tenant_id, source, event_id),
  KEY idx_events_tenant_type_time (tenant_id, event_type, occurred_at),
  KEY idx_events_tenant_dedup (tenant_id, dedup_key, dedup_bucket),
  CONSTRAINT fk_events_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE workflows (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(128) NOT NULL,
  version INT NOT NULL DEFAULT 1,
  status VARCHAR(32) NOT NULL DEFAULT 'draft',
  definition_json JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_workflows_tenant_name_version (tenant_id, name, version),
  KEY idx_workflows_tenant_status (tenant_id, status),
  CONSTRAINT fk_workflows_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE rules (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(128) NOT NULL,
  event_type VARCHAR(128) NOT NULL,
  priority INT NOT NULL DEFAULT 100,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  stop_on_match TINYINT(1) NOT NULL DEFAULT 0,
  condition_json JSON NOT NULL,
  workflow_id BIGINT UNSIGNED NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_rules_tenant_event (tenant_id, event_type, enabled, priority),
  CONSTRAINT fk_rules_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_rules_workflow FOREIGN KEY (workflow_id) REFERENCES workflows(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE workflow_runs (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT UNSIGNED NOT NULL,
  event_id BIGINT UNSIGNED NOT NULL,
  workflow_id BIGINT UNSIGNED NOT NULL,
  trigger_rule_id BIGINT UNSIGNED NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_runs_tenant_status (tenant_id, status, created_at),
  CONSTRAINT fk_runs_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id),
  CONSTRAINT fk_runs_event FOREIGN KEY (event_id) REFERENCES events(id),
  CONSTRAINT fk_runs_workflow FOREIGN KEY (workflow_id) REFERENCES workflows(id),
  CONSTRAINT fk_runs_rule FOREIGN KEY (trigger_rule_id) REFERENCES rules(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE workflow_tasks (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  run_id BIGINT UNSIGNED NOT NULL,
  node_key VARCHAR(64) NOT NULL,
  task_type VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  retry_count INT NOT NULL DEFAULT 0,
  max_retry INT NOT NULL DEFAULT 3,
  last_error TEXT NULL,
  scheduled_at DATETIME(3) NOT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_tasks_idempotency (idempotency_key),
  KEY idx_tasks_run_status (run_id, status),
  KEY idx_tasks_schedule (status, scheduled_at),
  CONSTRAINT fk_tasks_run FOREIGN KEY (run_id) REFERENCES workflow_runs(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE outbox_events (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT UNSIGNED NOT NULL,
  aggregate_type VARCHAR(64) NOT NULL,
  aggregate_id VARCHAR(64) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  payload_json JSON NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  retry_count INT NOT NULL DEFAULT 0,
  next_retry_at DATETIME(3) NULL,
  published_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_outbox_status_retry (status, next_retry_at, created_at),
  KEY idx_outbox_tenant_aggregate (tenant_id, aggregate_type, aggregate_id),
  CONSTRAINT fk_outbox_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO tenants (name, code, status)
VALUES ('Acme Corp', 'acme', 'active');

INSERT INTO workflows (tenant_id, name, version, status, definition_json)
VALUES (
  1,
  'high-value-order-timeout',
  1,
  'active',
  JSON_OBJECT(
    'nodes',
    JSON_ARRAY(
      JSON_OBJECT(
        'key', 'notify_webhook',
        'type', 'webhook',
        'config', JSON_OBJECT(
          'url', 'http://127.0.0.1:18081/receive',
          'method', 'POST'
        )
      )
    )
  )
);

INSERT INTO rules (tenant_id, name, event_type, priority, enabled, stop_on_match, condition_json, workflow_id)
VALUES (
  1,
  'order-timeout-high-value',
  'order.timeout',
  10,
  1,
  1,
  JSON_OBJECT(
    'logic', 'AND',
    'conditions',
    JSON_ARRAY(
      JSON_OBJECT('field', 'event_type', 'op', '=', 'value', 'order.timeout'),
      JSON_OBJECT('field', 'payload.amount', 'op', '>', 'value', 5000)
    )
  ),
  1
);
