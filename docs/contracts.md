# Execution contract

## Current baseline

Business routes require database API keys; tenant query/body fields cannot override credential-derived tenancy. Scopes and audit identify admitted operations. Health/metrics must be on a protected operations network.

## State machines and invariants

- Event: received or dedup_skipped. Unique (tenant, source, event_id); conflicting content returns 409. The dedup claim and event/tasks/outbox commit in one transaction.
- Run: pending -> running -> success/failed. A successful run requires all tasks to succeed. Static validated DAG dependencies unlock only after every parent succeeds; failed descendants are skipped.
- Task: pending -> running -> success, or retrying -> running, or failed. Outbox: pending -> processing -> published/failed; expired leases can be reclaimed.
- Delivery is at-least-once. A network timeout after remote acceptance can produce duplicates; receivers must honor Idempotency-Key. A lease token fences local settlement, not external effects.
- Batch is not atomic: 202 means every item accepted, 207 includes per-item failures. Never resubmit successful items with a new event identity.

## Migration and compatibility

Use numbered SQL migrations with a database advisory lock and SHA-256 checksum. Never edit an applied migration. MySQL DDL is not transactional: back up, test on a restored copy, stop on partial failure, and reconcile before retrying. Do not automatically run destructive down migrations. Demo seed updates are development-only and must not change production workflow versions.

OpenAPI 3.1 is stored in docs/openapi.json. This baseline describes existing routes; subsequent phases update the contract when behavior changes. Additive fields stay compatible within v1; credential requirements are an explicit upgrade boundary. Historical runs must retain their definition/version independently of newer deployments.

## Validation

Run go test ./..., go vet ./..., then the integration-tag tests against disposable MySQL/Redis. Linux CI runs race checks and container builds. Never use production rows for integration-test fixtures.
