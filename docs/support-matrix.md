# Support matrix

This matrix applies to release `v0.1.1`. "Supported" is the declared release
contract. "Verified" records the exact versions and environments exercised for
the v0.1.x line; it does not establish a minimum compatible Docker version.
Historical capacity inputs retain the original v0.1.0 candidate commit.
Exact patch download/runtime outcomes are recorded in the Release notes;
cross-compilation is not native validation.

## Runtime and toolchain

| Component | Supported range | Verified version | Verification |
|---|---|---|---|
| Go | 1.27.x for source builds | 1.27.2 | Windows amd64 native build/test; Linux amd64 GitHub Actions build, race test and vet |
| MySQL | 8.4 LTS | 8.4.11, `mysql@sha256:6ea90827b1100f8f2ae306a539f86d2c264a26ed435a2a9f75551dd5c3aeb242` | Linux amd64 container integration and migration tests |
| Redis | 7.4 | 7.4.11, `redis@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499` | Linux amd64 container integration tests with persistence enabled |
| Docker Engine | Current maintained release; no minimum claimed | 29.8.2 | Docker Desktop using the WSL2 Linux amd64 engine |
| Docker Compose | Compose v2 plugin; no minimum claimed | 5.5.1 | Full local stack and isolated smoke environment |
| OPA | Not applicable | Not applicable | The event platform does not embed OPA |
| Trivy | Not applicable | Not applicable | The event platform does not embed Trivy |
| Cosign | Not applicable | Not applicable | The event platform does not invoke Cosign at runtime |

## Platforms

| Platform | Status for v0.1.x | Evidence |
|---|---|---|
| Linux amd64 | Native CI and container runtime verified | Ubuntu 24.04 GitHub-hosted runner; Docker Desktop WSL2 local smoke |
| Windows amd64 | Native source build/test verified | Windows host with Go 1.27.2 |
| Linux arm64 | Cross-compile only | `CGO_ENABLED=0` release archive build |
| Windows arm64 | Cross-compile only | `CGO_ENABLED=0` release archive build |
| macOS amd64 | Cross-compile only | `CGO_ENABLED=0` release archive build |
| macOS arm64 | Cross-compile only | `CGO_ENABLED=0` release archive build |

Cross-compilation proves that the binaries build for a target. It does not
claim native runtime verification. MySQL and Redis remain required external
services for packaged binaries.

Run `bash scripts/check-support-matrix.sh` or
`./scripts/check-support-matrix.ps1` after changing a pinned version.
