# Architecture

## Components

- `http-server`: validates and ingests events, evaluates rules, and commits event/run/task/outbox state.
- `worker`: claims Outbox rows with a lease and invokes Webhook tasks.
- `migrate`: applies checksum-tracked SQL migrations under a MySQL advisory lock.
- `webhook-sink`: local-only receiver used by the smoke test.
- MySQL: source of truth for tenant data, dedup claims, workflows, executions, and Outbox.
- Redis: best-effort fast path for transport idempotency. Correctness does not depend on Redis.

## Write transaction

The ingestion transaction inserts the event first, then attempts a unique `event_dedup_claims` row. A duplicate claim changes the new event to `dedup_skipped` and creates no run. A successful claim creates matching runs, tasks, and Outbox rows in the same transaction.

The event identity key is `(tenant_id, source, event_id)`. The business dedup key is `(tenant_id, dedup_key, dedup_bucket)`. Events without a business key use their fingerprint and therefore are only suppressed by transport idempotency.

## Delivery transaction

A worker gives ready tenants round-robin opportunities under a durable scheduler cursor, enforces tenant live-lease/rate limits, then locks run -> outbox -> task using SKIP LOCKED. A claim increments the total attempt count, sets a lease token and marks task/run running. Settlement uses the same run -> outbox -> task order and requires the matching unexpired lease. All-parent dependency completion unlocks blocked tasks once; failure skips descendants without canceling independent roots. Workflow JSON is immutable source data; runtime edges are execution state.

A processing row whose lease expires can be reclaimed. This protects availability after worker crashes and fences late completion from the old worker.

## Trust boundary

Business routes authenticate database API keys carrying tenant, subject, scopes and expiry, checked on every request. Tenant selectors cannot override that identity. Privileged key issuance uses a database-access operator CLI. TLS and private operations endpoints remain deployment requirements; no SSO/RBAC management UI is provided.

Production Webhooks require exact hosts, HTTPS 443 and tenant secret references. DNS answers are checked and dialing uses the checked address; redirects/proxy/private IPs are refused. Automatic retries retain one delivery key, while audited replay starts a new run and new keys. The remote receiver owns effect idempotency. Traces propagate across durable Outbox and HTTP; enabling an OTLP endpoint activates export.

Migration application is separate from serving. Dirty/checksum/newer-schema checks block incompatible startup. See release-and-operations.md for backup, upgrade and restore boundaries.
