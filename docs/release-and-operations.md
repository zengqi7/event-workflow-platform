# Release, upgrade and recovery

## Compatibility

This is a pre-1.0 reference implementation, without a production SLA or a zero-downtime schema guarantee. API v1 may add fields; clients should ignore unknown fields. Auth/trust requirements and schema contracts can change only with an explicit upgrade note. Use reviewed immutable revisions for CI Actions and digest-pinned images.

SQL migrations are append-only. API/worker startup verifies every embedded migration checksum, dirty flag and unknown newer migration. Run the migration binary from the same revision before startup. Never downgrade an old binary onto a database containing newer migrations. No automatic destructive down migration is provided.

## Release artifacts

The release workflow validates the exact source (unit/race, vet, govulncheck, real MySQL integration, Compose and Docker build), then packages Linux/macOS/Windows amd64/arm64 binaries. Packaging uses CGO_ENABLED=0 and includes docs; external services are still required. Manual workflow_dispatch creates snapshot artifacts only. A reviewed v* tag creates a **draft**, which a maintainer reviews before publishing.

Checksums detect corruption, not publisher identity. Verify downloaded packages with both checksums and GitHub build provenance:

```sh
sha256sum -c checksums.txt
gh attestation verify ./PACKAGE.tar.gz --repo zengqi7/event-workflow-platform
```

The GitHub attestation proves this repository/workflow built these archives. It does not attest a business scan, establish a SLSA level, prove runtime safety, or make archive bytes reproducible across timestamps. Cross-compilation does not replace native runtime validation. No release tag or public release is created by merely merging this phase.

Linux/macOS: extract tar.gz, set environment variables, execute bin/migrate then the API and worker in separate supervised processes. Windows: Expand-Archive, set $env variables, run the matching bin/*.exe; repository scripts use PowerShell. Docker Compose v2 is the most uniform development path. Use go.mod's Go version for source builds.

## Upgrade procedure

Local Compose gives only the one-shot migrate service a root connection because MySQL binary logging requires privileged trigger creation. API/worker use the separate application account. Production should supply a dedicated reviewed DDL account and restrict runtime grants; do not disable binlog checks globally or grant SUPER to runtime processes. This local root connection is not a production credential-management design.

1. Pin the current revision/image and record migration names/checksums. Confirm tested capacity, TLS, outbound network rules, secret mounts and UTC clock synchronization.
2. Stop admission and workers. Wait for active requests to drain; outstanding leases can recover after restart. Do not run old/new workers concurrently during this upgrade.
3. Back up MySQL and operator-managed configuration/secrets. Encrypt and restrict the backup. Perform restoration and migration on an isolated instance first.
4. Run the new cmd/migrate once with a DDL-capable operator identity. Use least-privilege runtime identities afterward. DDL may partially commit: any error leaves dirty metadata and must stop the rollout.
5. Start one API/worker pair; check readiness and run smoke using a dedicated tenant/short-lived key. Validate original evidence and business state before scaling.
6. Retain backup/revision, observe queues/errors and verify cleanup of temporary credentials. Do not claim an RPO/RTO until measured.

For an untracked legacy schema, compare SHOW CREATE TABLE, indexes, FKs, triggers and seed data with the original migration on a restored copy first. Table existence/count is not proof of compatibility. If drift exists, reconcile deliberately or restore a clean known baseline. Only after verified equivalence may an operator record the exact original migration name/SHA-256 as a clean baseline. Never copy a checksum just to silence an error.

## Backup and restore exercise (PowerShell + Compose)

Run from the repository with the operator's chosen backup directory. These commands use the container's database name and root password; do not place passwords on host command lines. The SQL dump includes governance/audit data and must be treated as sensitive.

```powershell
New-Item -ItemType Directory -Force -Path 'E:\Backups' | Out-Null
docker compose stop api worker
$mysql = docker compose ps -q mysql
docker exec $mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --routines --triggers --events --no-tablespaces --set-gtid-purged=OFF --result-file=/tmp/platform-backup.sql "$MYSQL_DATABASE"'
if ($LASTEXITCODE -ne 0) { throw 'Backup failed' }
docker cp "${mysql}:/tmp/platform-backup.sql" 'E:\Backups\platform-backup.sql'
if ($LASTEXITCODE -ne 0) { throw 'Backup copy failed' }
```

Create a separate MySQL 8.4 instance/database, set $restoreContainer to that verified test instance, and restore the dump there. Do not point restoration at the live instance.

```powershell
docker cp 'E:\Backups\platform-backup.sql' "${restoreContainer}:/tmp/platform-backup.sql"
if ($LASTEXITCODE -ne 0) { throw 'Restore copy failed' }
docker exec $restoreContainer sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot "$MYSQL_DATABASE" < /tmp/platform-backup.sql'
if ($LASTEXITCODE -ne 0) { throw 'Restore failed' }
```

Check row counts, migration checksums, critical relationships and tenant isolation. Run the matching migration/API/worker against this restored copy and execute a synthetic smoke with network effects isolated. A completed dump without a tested restoration is not a proven backup.

If DDL fails, retain logs/dirty state, inspect the exact statement and compare schema to the tested restored copy. Repair and reconcile only after identifying all committed effects, or restore the whole verified backup. Do not clear dirty blindly, edit an applied SQL file, delete data volumes, or replay all statements without inspection.

## Rollback and incident boundaries

Before a schema change, reverting configuration/image to the previous tested revision may be sufficient. After a schema change, old binaries are refused; prefer a forward fix. Full restoration to a pre-upgrade database and matching revision loses writes since that backup and may repeat external effects. Require an operator-approved recovery window and reconcile downstream state; never describe restoring a backup as zero-loss rollback.

Monitor readiness, DB errors, queue age, expired leases, retry/failure counts and disk growth. Protect health/metrics on a private network. There is no automatic retention policy or managed backup/alerting service.

## Event-specific recovery

MySQL owns idempotency/dedup/DAG/outbox state. Redis is only a cache: a restored Redis snapshot newer than MySQL may return an event identity absent from the restored DB. Stop writers and start with an empty cache namespace/instance after DB restoration rather than treating Redis as durable business evidence. Restore tenant secrets separately to the read-only operator secret directory. Replay requires scope/reason/new key and creates new delivery keys; review receiver side effects first. Legacy reconstructed runs cannot be replayed. Alert on queue age and expired leases using API :8080 and worker :9091 metrics. Scheduler admission serialization is a measured future scaling boundary.
