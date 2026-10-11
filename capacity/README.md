# Reproducible capacity harness

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
