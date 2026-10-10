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

A worker selects eligible Outbox rows with `FOR UPDATE SKIP LOCKED`, assigns a random lease token, and marks the task/run running in one transaction. After delivery, `FinishDelivery` locks the Outbox row and verifies the lease token before atomically updating task, Outbox, and run status.

A processing row whose lease expires can be reclaimed. This protects availability after worker crashes and fences late completion from the old worker.

## Trust boundary

The V1 API is intended behind an internal gateway. Tenant selectors are not authentication credentials. Public deployment requires gateway authentication and authorization or the planned API-key/RBAC module.
