# Replay and Webhook security

## Recovering a failed business workflow

An operator resolves a receiving service outage and submits `POST /api/v1/workflow-runs/{id}/replay` with a scoped Bearer credential, `Idempotency-Key` header and `{"reason":"Receiver recovered; repeat the full workflow"}`. Responses contain `run_id`, `generation` and `reused`.

The original run must be finished and have a verified runtime snapshot. Replay copies that definition and the original event payload, creates new tasks/dependency edges/outbox entries, and starts only root nodes. The new run links to the original root through `replay_of`; generations increase under the tenant lock. The request mapping and success audit are committed with the new run. The same tenant/key/source/reason returns the previous result; changing source or reason returns 409. Cross-tenant runs return 404. Pending-task quotas include blocked nodes.

Automatic retry keeps the original task and `Idempotency-Key`. Replay creates a new delivery key for each node. Both remain **at-least-once**: the receiving service must deduplicate delivery keys. Replay repeats successful branches too and may repeat business effects; use only after reviewing those effects. Partial replay and compensating transactions are not implemented. `legacy_reconstructed` snapshots are rejected because backfilled definitions cannot establish historical fidelity.

## Production Webhooks

- Set `WEBHOOK_ALLOWED_HOSTS` to comma-separated exact destination hostnames. Wildcards are unsupported. An empty list denies all deliveries.
- Production requires HTTPS on port 443, no userinfo/fragment, no redirects, no environment proxy, and public DNS answers. Every DNS answer is checked at connection time; the socket dials that checked IP while TLS still verifies the destination hostname. Private, loopback, link-local, shared, benchmarking and documentation ranges are denied. Enforce an outbound firewall as defense in depth.
- Define `config.secret_ref`, for example `billing-v1`. Mount a read-only operator-controlled `WEBHOOK_SECRET_DIR` with files at `<root>/<tenant-id>/<secret_ref>`. Files contain 32 to 4096 raw secret bytes; avoid trailing newlines unless intentionally part of the key. Tenant directories cannot be symlinks; file opens are confined to the tenant directory. Never give API clients filesystem write access to this mount. Secret values are never stored in workflow JSON, outbox or logs.
- Headers: `X-Webhook-Timestamp` is Unix seconds; `X-Webhook-Signature` is `v1=<hex HMAC-SHA256(secret, timestamp + "." + raw_body)>`. Receivers verify the MAC with a constant-time comparison, reject timestamps outside a configured tolerance (e.g. 300 seconds), and deduplicate `Idempotency-Key`. A retry has a fresh timestamp/signature but the same delivery key. This project signs outbound requests; receiver validation belongs to the receiver.
- Rotate by provisioning a new secret reference and publishing a new immutable workflow version. Retain old references while retries/replays remain allowed. Historical runs store the reference, not a frozen key value; deleting a referenced secret prevents delivery.

Compose explicitly opts into unsigned HTTP for the `webhook-sink` hostname using `WEBHOOK_ALLOW_DEVELOPMENT=true` in the `docker` environment. That exception cannot be enabled in the production environment. TLS certificates are always verified, including in development HTTPS tests.

## Validation

Run integration tests against a disposable MySQL instance: `go test -count=1 -tags=integration ./integration`. The replay test creates and drops its own schema, verifies concurrent idempotent requests, new delivery keys, full DAG reset, generation allocation and tenant isolation. Notifier tests cover signing, redirect refusal, default denial, private-address rejection and tenant secret confinement. Run `go test -race -tags=integration ./...` on Linux to exercise symlink confinement.
