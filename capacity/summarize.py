#!/usr/bin/env python3
import argparse
import json
import math
import pathlib
import statistics


def percentile(values, fraction):
    if not values:
        return None
    ordered = sorted(values)
    index = (len(ordered) - 1) * fraction
    lower = math.floor(index)
    upper = math.ceil(index)
    if lower == upper:
        return ordered[lower]
    return ordered[lower] + (ordered[upper] - ordered[lower]) * (index - lower)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=pathlib.Path)
    args = parser.parse_args()
    runs = []
    for run_file in sorted(args.root.glob("*/run.json")):
        run = json.loads(run_file.read_text(encoding="utf-8-sig"))
        folder = run_file.parent
        k6 = json.loads((folder / "k6-summary.json").read_text(encoding="utf-8"))
        values = [float(value) for value in (folder / "completion-latency-ms.txt").read_text().split()]
        iterations = k6["metrics"]["iterations"]["values"]["count"]
        duration = k6["state"]["testRunDurationMs"] / 1000
        run.update({"accepted_throughput_rps": iterations / duration, "accepted_p95_ms": k6["metrics"]["accepted_latency"]["values"]["p(95)"], "completion_p95_ms": percentile(values, 0.95), "completed": len(values)})
        runs.append(run)
    throughput = [run["accepted_throughput_rps"] for run in runs]
    cv = statistics.stdev(throughput) / statistics.mean(throughput) if len(throughput) > 1 and statistics.mean(throughput) else 0
    result = {"runs": runs, "median_accepted_throughput_rps": statistics.median(throughput) if throughput else None, "throughput_cv": cv, "unstable": cv > 0.10}
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
