# Persistent DAG execution

Ingest creates every task and one unique outbox per task in a transaction.
Only roots are pending; dependent tasks/outboxes start blocked. Runtime
`task_dependencies` stores unique run/parent/child edges. It is execution state,
not a second workflow definition. Nodes are case-sensitive and unique per run.

Successful settlement marks outgoing edges succeeded and unlocks blocked children
whose parents have all succeeded. Task, edges, outbox and run commit together.
Repeated settlements cannot decrement counters or emit duplicate tasks: the
processing token and unexpired database lease fence them, and blocked-to-pending
updates are conditional. Retries leave edges pending.

Terminal failure skips all blocked descendants; independent branches continue.
The run reports failed as soon as a task fails but finished_at is assigned only
when no blocked/pending/running/retrying tasks remain. A skipped task is never
claimed. This release implements all-parents-success dependencies, not conditional
branches, compensation or Temporal-style deterministic code replay.

All mutations acquire locks in run -> outbox -> task order. Candidate discovery
does not lock; each candidate rechecks eligibility after acquiring its run lock.
Workers skip busy run locks and do network delivery only after transaction commit.
Different branches can deliver concurrently; same-run settlements serialize.
Expired leases can be reclaimed, but stale completion is rejected even if no
replacement worker has claimed yet. Delivery remains at-least-once: receivers
must persist Idempotency-Key to avoid repeated side effects after a worker crash.

Apply 000005 before deploying E3 API/worker together. Rollback to E2 requires
draining DAG runs and disabling DAG rules; E2 workers do not understand blocked
dependencies. Keep migrations forward-only and take database backups.

`go test -race -tags=integration ./integration` creates isolated disposable
schemas, applies actual migrations, and verifies diamond fan-in, duplicate
settlement, descendant failure propagation, independent branches, expired leases
and reclamation. Existing demo data is untouched.
