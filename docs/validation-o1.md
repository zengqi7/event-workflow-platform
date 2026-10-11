# O1 validation evidence

Validation date: 2026-10-11. Local host: Windows amd64, Docker Desktop, Go 1.27.2, MySQL 8.4, Redis 7.4. Only disposable stage/smoke schemas were used; existing demonstration volumes were not deleted.

- Windows go test ./... and go vet ./... passed.
- Real MySQL/Redis integration passed; Linux Docker go test -race -count=1 -tags=integration ./... passed.
- Migration tests cover upgrade from the previous checksum-only tracker, repeat application with MaxOpenConns(1), dirty/checksum refusals, newer-schema refusals and no silent untracked-schema adoption.
- Fresh Compose deployment passed after separating privileged migration credentials from runtime credentials. MySQL binlog trigger creation requires a privileged migrator. The protected dirty state correctly rejected the initial unprivileged attempt.
- Demo seed INSERT SELECT now disambiguates workflows.id; repeated migration/seed invocation passed.
- HTTP smoke passed: repeat identity reuse, changed-payload 409, business dedup_skipped, one normal receiver delivery, successful run/task and published Outbox. This normal-path count is not an exactly-once guarantee.
- Worker readiness was HTTP-successful on isolated host port 19091.
- mysqldump with triggers/routines restored into an independent schema, then cmd/migrate verified it. Seven migrations were clean; restored data contained one received event, one dedup_skipped event, one successful run and one published Outbox row.
- govulncheck reported no vulnerabilities; release YAML passed actionlint and Compose/OpenAPI parsed.

Release archives are cross-compiled by scripts/package-release.sh. Native macOS/Windows ARM runtime execution and a real version-tag publication/attestation retrieval are not part of these local checks. No production RPO/RTO, throughput SLA or zero-downtime upgrade is asserted.
