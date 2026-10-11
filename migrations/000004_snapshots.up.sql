ALTER TABLE workflow_runs ADD COLUMN workflow_version INT NULL, ADD COLUMN definition_json JSON NULL, ADD COLUMN definition_digest CHAR(64) NULL, ADD COLUMN snapshot_origin VARCHAR(32) NOT NULL DEFAULT 'runtime';
UPDATE workflow_runs r JOIN workflows w ON w.id=r.workflow_id SET r.workflow_version=w.version,r.definition_json=w.definition_json,r.definition_digest=SHA2(CAST(w.definition_json AS CHAR),256),r.snapshot_origin='legacy_reconstructed';
DELIMITER $$
CREATE TRIGGER workflows_immutable BEFORE UPDATE ON workflows FOR EACH ROW
BEGIN
 IF NOT (NEW.definition_json <=> OLD.definition_json) OR NEW.version<>OLD.version OR NEW.tenant_id<>OLD.tenant_id OR NEW.name<>OLD.name THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='workflow version is immutable; publish a new version';
 END IF;
END$$
CREATE TRIGGER workflows_no_delete BEFORE DELETE ON workflows FOR EACH ROW
BEGIN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='workflow versions cannot be deleted';
END$$
DELIMITER ;
