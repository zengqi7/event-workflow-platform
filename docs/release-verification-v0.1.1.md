# v0.1.1 completed release verification

Verification date: 2026-10-11 UTC. This report records completed checks after
publication; the signed v0.1.1 tag contains the final verifier and protocol,
while this report is added in a separate documentation PR. It does not move
the tag or change release assets.

## Identity and publication

| Field | Verified value |
|---|---|
| Release | [v0.1.1](https://github.com/zengqi7/event-workflow-platform/releases/tag/v0.1.1), latest, non-Draft, non-Prerelease |
| Published | 2026-10-11T15:53:21Z |
| Source / workflow signer commit | `74ffc838f31e3c9e68641206aff45b099d048e03` |
| Signed annotated tag object | `2240cc82265b11ef80ccf886f379a994ddd9e04c` |
| Tag signature | local verification passed; GitHub verified=true, reason valid |
| Main CI | [38151920890](https://github.com/zengqi7/event-workflow-platform/actions/runs/38151920890), success |
| Snapshot release preflight | [38151926289](https://github.com/zengqi7/event-workflow-platform/actions/runs/38151926289), success |
| Tagged release workflow | [38152402873](https://github.com/zengqi7/event-workflow-platform/actions/runs/38152402873), success |
| Platform immutability | `isImmutable=true`; `gh release verify v0.1.1` passed |
| Asset release attestation | `gh release verify-asset` passed for all eight assets, including checksums.txt |

Both runtime tests and seven strict build-provenance checks passed while the
release was a Draft. Publication then locked the tag and eight-asset set.
The older v0.1.0 release ID, seven asset IDs/digests and signed tag object were
compared with the captured baseline and are unchanged. Its historical
`isImmutable=false` is not retroactively changed by the new repository setting.

## Download and benchmark audit

GitHub CLI 2.102.0 downloaded exactly six binary archives, benchmark-evidence.tar.gz
and checksums.txt into an empty directory. All seven SHA-256 values and GitHub
build attestations passed with source and signer commit pinned to the value
above, source ref `refs/tags/v0.1.1`, tagged workflow identity
`zengqi7/event-workflow-platform/.github/workflows/release.yml@refs/tags/v0.1.1`
and self-hosted runners denied. No trust or identity checks were bypassed.
Release attestation checks after publication additionally covered all eight
downloaded files, including the checksum file itself.

During the first post-publication pass, the local Sigstore verifier failed to
initialize trust while checking the final asset. The failed attempt was
retained. A complete second pass with an independent fresh E-drive cache
passed all eight assets; trust, identity and cryptographic checks stayed enabled.
This initialization failure did not indicate a checksum or identity mismatch.

| Archive | SHA-256 |
|---|---|
| benchmark-evidence.tar.gz | `36e6ea9c3a4c9c686ff0502047ca6f7a69f4c537322c5551395218a7ebcd831f` |
| darwin-amd64.tar.gz | `ee44855d5dc8c49168a1ea4551ccddd1cd5944059ae5d953bc4e6e588d89b219` |
| darwin-arm64.tar.gz | `4572d809e976153bbd6fee65a9a5dcc4a7da00fcca7fa85d9b91655dc055ab37` |
| linux-amd64.tar.gz | `869abaebeed7f8a907aaf14686af8a563b5d4bf321009b19b62725d1d2eae7f7` |
| linux-arm64.tar.gz | `8fe11983b054416f657caa425c8fcdd1ef6e6dd3cbb47f42bb926b7d1b73c15b` |
| windows-amd64.zip | `5bf4980b1e869eac980a24f795a9e64c27636ef46dfe13cb3d6ca4643fc904df` |
| windows-arm64.zip | `f3d087bd883f005f862c071b6de15da8c473ff429568f621d70f29166924ef89` |

Binary archive names have prefix `event-workflow-platform-v0.1.1-`.
The downloaded benchmark archive's internal hashes and derived resource/
latency/queue summaries passed `python capacity/evidence.py audit` after extraction.
Corrected CPU medians from 135 samples per role are API 3.18%, Worker 4.67%
and MySQL 13.75%; the standard odd median uses the 68th sorted sample.
Throughput, P95, peaks and queue recovery conclusions are unchanged.

The evidence retains 12 normal and six recovery runs at historical candidate
`58326ec93c088f1e64f7ce8b89faff96c010b1c8`. This patch did not perform fresh
v0.1.1 capacity measurement. HTTP admission P95 is the preserved k6 aggregate;
request-level HTTP samples were not retained. Completion latencies and resource
statistics can be independently recomputed.

## Downloaded-package runtime

| Platform | Result / completed UTC | Scope |
|---|---|---|
| Windows amd64 | Pass, 2026-10-11T15:51:26Z | native downloaded API/worker/migrate/key/inspect binaries |
| Linux amd64 | Pass, 2026-10-11T15:52:07Z | downloaded Linux binaries in isolated containers |

Each run used a fresh MySQL 8.4 tmpfs database, Redis 7.4, new isolated Docker
network, random loopback ports and a one-hour API key. The downloaded binaries
verified migrations/readiness, transport idempotency, payload conflict 409,
business deduplication, Webhook receipt, successful run/task, published Outbox,
inspect and rejection of the revoked credential. Temporary resources were
removed. No application binary was compiled locally.

Both tests used the verified Linux Webhook sink in the isolated network because
its fixed Windows host port was already occupied. This run does not establish
native Windows sink execution. macOS and arm64 archives were cross-compiled
but not natively exercised. External services remain required. Webhook
delivery is at-least-once and consumers must handle duplicate deliveries.
Short local tests are not a production SLA, and provenance proves build
origin rather than runtime safety or reproducible binaries.

Follow [the tagged verification protocol](https://github.com/zengqi7/event-workflow-platform/blob/v0.1.1/docs/release-verification.md),
[support matrix](support-matrix.md), [historical capacity report](capacity/v0.1.0.md)
and [auditable inputs](../capacity/evidence/v0.1.0). Operator JSON records and
logs remain local; sanitized benchmark evidence is public and checksummed.
