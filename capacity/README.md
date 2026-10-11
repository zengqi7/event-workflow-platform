# Reproducible capacity harness

## Public benchmark evidence

The checked-in `capacity/evidence/v0.1.0/` contains allowlisted calculation
inputs from the historical measurements, `summary.json`, `resources.json`,
`metadata.json` and SHA-256 checksums. It is a sanitized audit snapshot, not a
claim that a new patch release was capacity-tested. Source commits come from
the original report because the original harness did not record HEAD.

`evidence.json` retains per-run admission counts/duration/P95, all completion
latencies, queue samples and numeric resource series; raw-input hashes identify
the private originals. Tokens, DSNs, env files, logs, local paths, ports, scan IDs
and container identities are excluded. The historical k6 export only retained
HTTP P95, so HTTP P95 itself cannot be independently recalculated from request
samples; completion P95 and resource medians can be.

```powershell
python capacity/evidence.py audit capacity/evidence/v0.1.0
python capacity/evidence.py pack capacity/evidence/v0.1.0 results/benchmark-evidence.tar.gz
```

CI audits the snapshot and uploads the deterministic archive plus its checksum
as a `benchmark-evidence` Actions artifact (90 days). Release packaging includes
the same evidence with the release checksum manifest and build provenance for
long-term discovery. Publication of v0.1.1 does not alter v0.1.0 assets.

The harness uses the source and k6 image pinned in `lock.json`. Dataset
preparation and measurement are separate so no upstream network request occurs
during a run.

```powershell
E:\Anaconda\python.exe capacity\prepare_corpus.py E:\capacity-sources\octokit-webhooks-aa80c24.tar.gz
./capacity/run.ps1 -WorkerConcurrency 4 -Rate 20 -Duration 30s -Repetitions 3
```

Each run gets an isolated Compose project and fresh volumes. The override fixes
per-container CPU and memory limits. The runner records k6 submission latency,
database-backed completion latency, queue drain time and per-container resource
samples. Use worker concurrency 1, 4, 8 and 16 for the formal matrix.

The source repository is an archived Octokit fixture collection, not a claim
about current GitHub production traffic. Results describe this workload and the
documented test host only; they are not an SLA or production sizing promise.
