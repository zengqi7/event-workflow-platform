# Contributing

1. Create a focused branch from `main`.
2. Keep tenant filters and idempotency constraints intact in every data path.
3. Run `go test -race ./...` and `go vet ./...`.
4. For storage or delivery changes, run the integration test and
   `scripts/smoke-test.ps1` against the Compose stack.
5. Never commit `.env`, credentials, generated data, or local binaries.

Changes to migrations must be additive. Do not modify a migration that may
already have been applied because the migrator verifies checksums.
