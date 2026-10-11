ALTER TABLE workflow_tasks MODIFY node_key VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL, ADD UNIQUE KEY uk_task_run_node(run_id,node_key);
ALTER TABLE outbox_events ADD UNIQUE KEY uk_outbox_task(aggregate_type,aggregate_id);
CREATE TABLE task_dependencies (
 run_id BIGINT UNSIGNED NOT NULL,
 parent_task_id BIGINT UNSIGNED NOT NULL,
 child_task_id BIGINT UNSIGNED NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'pending',
 PRIMARY KEY(run_id,parent_task_id,child_task_id),
 KEY idx_dependency_child(run_id,child_task_id,status),
 CONSTRAINT fk_dependency_run FOREIGN KEY(run_id) REFERENCES workflow_runs(id),
 CONSTRAINT fk_dependency_parent FOREIGN KEY(parent_task_id) REFERENCES workflow_tasks(id),
 CONSTRAINT fk_dependency_child FOREIGN KEY(child_task_id) REFERENCES workflow_tasks(id)
) ENGINE=InnoDB;
