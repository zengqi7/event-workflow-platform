# v0.1.0 release verification

Verification date: 2026-10-11. The release was kept as a draft until every
gate in this document passed. Evidence describes this tagged release only;
later commits on `main` do not change the tag or the assets already verified.

## Release identity

| Field | Verified value |
|---|---|
| Tag | `v0.1.0` |
| Annotated tag object | `9835c76884a9d33663486a8716f8aa4e616df048` |
| Source commit | `df5d13d5a0c30aed7793d3e0561a46ca475620ad` |
| Tag signature | GitHub `verified=true`, reason `valid` |
| Signing key | Ed25519 `SHA256:P1DZmNa98gkb+Dc5lTo3qET8AetjmUgKTORFni5F+zg` |
| Release workflow | [run 38145479479](https://github.com/zengqi7/event-workflow-platform/actions/runs/38145479479), successful |
| Tagged-source CI | [run 38143429888](https://github.com/zengqi7/event-workflow-platform/actions/runs/38143429888), successful |

`git verify-tag v0.1.0` verified the same key locally. The release workflow's
exact-tag/main preflight, unit and race tests, vet, vulnerability scan, real
integration tests, container build, packaging, checksums and build-attestation
jobs all passed.

GitHub reports `isImmutable=false` because immutable releases are not enabled
for this repository. Maintainers must therefore treat the signed tag and the
verified asset set as append-only and never replace or move them. Consumers
must still verify both checksums and attestations after every download.

## Download verification

All assets were downloaded into a new directory. The verifier required the
exact repository, source commit, tagged workflow identity
`zengqi7/event-workflow-platform/.github/workflows/release.yml@refs/tags/v0.1.0`,
workflow signer commit, source tag ref, GitHub-hosted runner and GitHub OIDC
issuer. It did not bypass TUF metadata, certificate, transparency-log or
identity checks. Six of six checksums and six of six GitHub build attestations
passed.

| Archive | SHA-256 |
|---|---|
| `event-workflow-platform-v0.1.0-darwin-amd64.tar.gz` | `bd664905b9a4eb010bec1741a1de88625001814250840209c999c7fd91b43a1e` |
| `event-workflow-platform-v0.1.0-darwin-arm64.tar.gz` | `2fe5eb77020922f95225b4cd211d8bddf369ebc28aff79838d7120634502fc75` |
| `event-workflow-platform-v0.1.0-linux-amd64.tar.gz` | `87cb8a7e2b64d45c465992c35175685a8c58404a4f011f33ce58095d44f54e53` |
| `event-workflow-platform-v0.1.0-linux-arm64.tar.gz` | `24d29e261b9375521ad3dda186ffd31aad6d99e388d0d567ff6ba71f2db09097` |
| `event-workflow-platform-v0.1.0-windows-amd64.zip` | `ceb40609b99c6b28e17756eb53d6873c9f17afc1395d9f82b19073098750cedb` |
| `event-workflow-platform-v0.1.0-windows-arm64.zip` | `095a41e79fdfe92492997f089fdc62eae4578e2dd3aa469fb3ec7f0bfdc88b0c` |

The command and failure policy are documented in
[release-and-operations.md](release-and-operations.md). The verifier summary
and per-asset attestation JSON are operator evidence and are intentionally not
committed as mutable absolute-path output.

## Clean-room runtime

The downloaded Windows and Linux amd64 archives were extracted separately.
Each run used a new isolated Docker network, fresh MySQL 8.4 tmpfs instance,
fresh Redis 7.4 instance, random loopback ports and a one-hour API key. The
release's own `migrate`, `key`, API, worker and `inspect` binaries ran the
scenario. The key was revoked and rejected before teardown.

| Archive runtime | Result | Verified behavior |
|---|---|---|
| Windows amd64 | Pass | fresh migrations and readiness; transport idempotency; changed-payload HTTP 409; business-key dedup; one Webhook receipt; successful run/task; published Outbox; inspect output; credential revocation |
| Linux amd64 | Pass | same checks, with all application binaries running from the downloaded Linux archive |

The Windows scenario used the downloaded Linux amd64 `webhook-sink` helper in
an isolated container because another service owned the helper's fixed host
port. The API, worker, migration, key and inspect binaries under test remained
native Windows amd64. This does not weaken the application-binary runtime
claim, but it is not a native Windows test of the optional sink helper.

## Capacity and limits

The reproducible capacity matrix, P95 latency, queue recovery and resource
measurements are in [capacity/v0.1.0.md](capacity/v0.1.0.md). The normal test
offered 8 events/s; median completed throughput ranged from 3.358 to 4.692
runs/s as worker concurrency increased. Backlog and crash recovery each passed
three repetitions. These measurements are regression evidence, not an SLA.

macOS, arm64 and Windows arm64 archives passed cross-compilation, checksums and
provenance only; no native runtime execution is claimed. Runtime smoke used a
local receiver and does not cover third-party latency, multi-host workers,
network partitions, MySQL/Redis failover, production TLS, managed secrets or
long-duration soak behavior. Delivery remains at-least-once.
