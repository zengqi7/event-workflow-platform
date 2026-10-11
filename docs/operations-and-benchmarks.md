# Scheduling, observability and failure exercises

## Tenant isolation

`scheduler_state` stores a round-robin cursor shared by all workers. A claim gives one ready tenant an opportunity before moving to the next, rather than always serving the globally oldest task. Each transaction takes locks in the order scheduler -> tenant -> run -> outbox -> task. Settlement uses run -> outbox -> task; ingestion/replay use tenant -> new run. Run locks use `SKIP LOCKED`; transactions never wait for an existing run while holding a tenant lock.

Tenant defaults are 8 live task leases and 600 dispatches per UTC minute. These are separate from HTTP admission/pending-task quotas. Operators adjust `tenants.max_concurrent_tasks` and `dispatches_per_minute`. Every claim/reclaim counts toward the dispatch rate and task `attempt_count`; `max_retry` remains the legacy column name for the total attempt budget (default 3), not three additional retries. `retry_count` counts settled failures. A crashed final attempt becomes failed on the next eligible poll and its blocked descendants are skipped. Fairness is by opportunity, not equal CPU time; rate windows are fixed minutes and can burst near a boundary. The concurrency bound applies to live leases, not to remote effects that outlive HTTP timeout.

The scheduler row deliberately serializes brief admission transactions; this is a simple correctness tradeoff and a possible throughput bottleneck. Candidate lookup examines at most 100 ready tenants per transaction; the durable cursor advances even when quotas prevent a claim. A locked head run can defer that tenant until a later poll. Weighted fairness, sharded admission and adaptive limits are future work, requiring measurements first.

## Worker and traces

The worker polls once per second with `WORKER_CONCURRENCY` (default 4, range 1..16). Each goroutine claims immediately before delivery and processes up to five tasks per cycle. HTTP delivery has a 10-second upper bound, the HTTP client a 5-second timeout, and leases last 60 seconds. Termination stops new polls/cancels requests; unsettled claims recover through lease expiry. API HTTP shutdown drains for up to 10 seconds.

Worker `/metrics` and `/readyz` listen on `WORKER_METRICS_ADDR` (default `127.0.0.1:9091`). Compose publishes that port only on host loopback. Scrape worker counters there and API ingestion counters at API `/metrics`; counters are process-local and reset on restart. Worker queue gauges come from MySQL: outbox states, expired leases and age of the oldest ready item. Protect metrics behind an internal network/proxy in a multi-host deployment; no tenant IDs or event payloads become metric labels.

Set `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` to enable the OTLP/HTTP exporter. Without either endpoint, tracing is a no-op. Configure exporter TLS/auth with standard OTel environment settings. Parent-based sampling samples 10% of new roots. W3C `traceparent`/`tracestate` are extracted at HTTP ingress, stored in outbox payloads, restored into consumer spans after restart, and injected into Webhook headers. Replay starts a new operation trace; business `trace_id` remains a separate correlation field. Spans do not include payloads, credentials or secret contents.

## Reproducible checks

- `go test -race -count=1 -tags=integration ./...`: isolated MySQL schemas test noisy-neighbor concurrency/rate isolation, DAG fan-in, lost leases, crash-budget exhaustion and a receiver accepting a request before settlement is lost. The latter delivers twice but the receiver fixture applies one effect using the stable delivery key; it proves the need for receiver cooperation.
- `go test -tags=integration ./integration -run '^$' -bench '^BenchmarkClaimReadyTask$' -benchtime=100x -count=1`: measures one transactional claim against real MySQL. Fixture reset happens outside the timer. This is a synthetic single-tenant/single-ready-task latency benchmark, excluding Webhook, ingestion, settlement and load growth. It cannot establish production QPS or latency percentiles. Record Go/OS/CPU/DB and deployment topology with any result.
- `go test -tags=integration ./integration -run 'TestDurableTracePropagationToWebhook'`: verifies one trace survives the durable outbox and reaches the receiver.

Observe queue age and expired leases during outages. If the worker dies, wait for lease recovery rather than resetting DB rows manually. Resolve failed dependencies/receiving services and use audited replay for a reviewed full rerun. If MySQL is unavailable, readyz/metrics fail and no success state is fabricated.

### Local sample (2026-10-11)

Windows/amd64, Intel i5-12500H, Go 1.27.2, MySQL 8.4 in Docker Desktop accessed over the host published port; `-benchtime=100x -count=1`: 24.53 ms/claim, 17,050 B/op, 337 allocs/op. This single run is environment-sensitive and includes multiple SQL round trips through Docker networking. It is a reference sample, not a throughput/SLO claim; compare like-for-like repeated samples before optimizing.
