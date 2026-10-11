# Immutable workflow versions

`POST /api/v1/workflows` requires `workflows:write`. Tenant identity comes from the
credential, never the body. Publish an explicit positive version; an existing
`(tenant,name,version)` returns 409. Optional `rule_id` switches an existing rule
of the same tenant in the publication transaction. Unknown/foreign rules return
404. Publication success is audited separately from HTTP admission.

```json
{"name":"order-reminder","version":1,"status":"active","rule_id":1,
 "definition":{"nodes":[{"key":"notify","type":"webhook",
 "config":{"url":"https://receiver.example/hooks","method":"POST"}}]}}
```

Definitions contain 1-100 unique nodes. Supported types: webhook; methods: POST
(default), PUT. `depends_on` is checked for missing/duplicate/self edges and cycles
regardless of node order. E3 executes dependent nodes through persisted DAG edges.
Inline `secret` is forbidden; secret references are wired in E4.
URL syntax validation is not SSRF protection; outbound enforcement follows in E4.

Database triggers prohibit definition/name/tenant/version mutation and deletion.
Retire a version by changing status to draft; publish a new version for edits.
Each new run records version, definition, canonical SHA-256 and `runtime` origin.
Digest uses Go JSON encoding of the typed definition (including JSON numbers),
not MySQL's whitespace-normalized JSON representation. Reparse with UseNumber
and encode before checking a runtime digest.

Migration 000004 reconstructs legacy run snapshots from the currently stored
version, marks `legacy_reconstructed`, and hashes MySQL's stored JSON rendering.
These records cannot prove the original definition at historical execution time.

Development `SEED_WEBHOOK_URL` publishes demo version 2 once and rebinds only
the original Acme demo rule still pointing at version 1. Rerunning never changes
version 2's URL or a rule bound to a later/custom version. Publish a new version
to change the destination; production migration refuses demo seed configuration.

Run `go run ./cmd/migrate` before API/worker startup. Back up before DDL; MySQL
DDL does not roll back. Do not drop columns/triggers as an application rollback.
