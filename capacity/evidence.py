#!/usr/bin/env python3
"""Export, audit and package allowlisted capacity evidence without raw credentials."""
import argparse
import gzip
import hashlib
import io
import json
import math
import pathlib
import re
import statistics
import tarfile

from resources import aggregate, finite_number, read_samples

RUN_FIELDS = ("run_id", "worker_concurrency", "worker_replicas", "batch_size",
              "rate", "duration", "completed_count", "processing_window_seconds",
              "completion_throughput_rps", "recovery_mode", "crashed_at",
              "recovery_started_at", "queue_drain_seconds", "completed_at")
FILES = ("evidence.json", "summary.json", "resources.json", "metadata.json")


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True, allow_nan=False) + "\n", encoding="utf-8", newline="\n")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def percentile(values, fraction):
    if not values:
        raise ValueError("empty latency input")
    values = sorted(values)
    index = (len(values) - 1) * fraction
    lo, hi = math.floor(index), math.ceil(index)
    return values[lo] + (values[hi] - values[lo]) * (index - lo)


def export_run(folder, source_sha):
    raw = read_json(folder / "run.json")
    run = {key: raw[key] for key in RUN_FIELDS if key in raw}
    if not re.fullmatch(r"(?:event|cloud)-[a-zA-Z0-9-]+", run["run_id"]):
        raise ValueError("invalid run id")
    k6 = read_json(folder / "k6-summary.json")
    completed = [finite_number(value) for value in
                 (folder / "completion-latency-ms.txt").read_text().split()]
    queue = [{key: sample.get(key) for key in ("timestamp", "terminal", "total")}
             for sample in map(json.loads, (folder / "queue-samples.jsonl").read_text(encoding="utf-8-sig").splitlines())]
    if len(completed) != raw["completed_count"]:
        raise ValueError("completed latency count mismatch")
    if not queue or queue[-1]["terminal"] != queue[-1]["total"] or queue[-1]["total"] != len(completed):
        raise ValueError("queue did not reach the recorded terminal count")
    inputs = ("run.json", "k6-summary.json", "completion-latency-ms.txt",
              "queue-samples.jsonl", "resource-samples.jsonl")
    return {"run": run, "source_commit": raw.get("source_commit", source_sha),
            "source_commit_basis": "harness" if "source_commit" in raw else "historical capacity report",
            "admission": {"iterations": k6["metrics"]["iterations"]["values"]["count"],
                          "duration_ms": k6["state"]["testRunDurationMs"],
                          "p95_ms": k6["metrics"]["accepted_latency"]["values"]["p(95)"]},
            "completion_latency_ms": completed, "queue_samples": queue,
            "resource_samples": read_samples(folder / "resource-samples.jsonl"),
            "raw_input_sha256": {name: digest(folder / name) for name in inputs}}


def derive(evidence):
    summaries, resources = {}, {}
    for group, runs in evidence["groups"].items():
        summaries[group] = []
        for item in runs:
            run = item["run"]
            admission = item["admission"]
            if admission["duration_ms"] <= 0:
                raise ValueError("invalid admission duration")
            summaries[group].append({**run, "source_commit": item["source_commit"],
                "accepted_throughput_rps": admission["iterations"] / (admission["duration_ms"] / 1000),
                "accepted_p95_ms": admission["p95_ms"],
                "completion_p95_ms": percentile(item["completion_latency_ms"], 0.95),
                "completed": len(item["completion_latency_ms"])})
        resources[group] = aggregate([sample for item in runs for sample in item["resource_samples"]])
    matrix = {}
    for run in summaries["normal"]:
        key = f"workers={run.get('worker_concurrency', run.get('worker_replicas'))};batch={run.get('batch_size', 'n/a')}"
        matrix.setdefault(key, []).append(run)
    rows = {}
    for key, runs in matrix.items():
        admitted = [run["accepted_throughput_rps"] for run in runs]
        completed = [run["completion_throughput_rps"] for run in runs]
        def cv(values):
            return statistics.stdev(values) / statistics.mean(values) if len(values) > 1 else 0
        rows[key] = {"repetitions": len(runs),
            "accepted_median_rps": statistics.median(admitted),
            "completed_median_rps": statistics.median(completed),
            "accepted_worst_p95_ms": max(run["accepted_p95_ms"] for run in runs),
            "completion_worst_p95_ms": max(run["completion_p95_ms"] for run in runs),
            "accepted_cv": cv(admitted), "completion_cv": cv(completed)}
    return {"schema_version": 1, "groups": summaries, "normal_matrix": rows,
            "images": evidence["images"]}, {"schema_version": 1, "groups": resources,
            "median_method": "statistics.median; workers are per-container samples"}


def export(args):
    if not re.fullmatch(r"[a-f0-9]{40}", args.source_commit):
        raise ValueError("source commit must be a full SHA")
    if args.output.exists():
        raise ValueError("output must be a new directory")
    groups = {}
    for label, root in (("normal", args.normal), ("recovery", args.recovery)):
        runs = [export_run(path.parent, args.source_commit) for path in sorted(root.glob("*/run.json"))]
        if not runs:
            raise ValueError(f"no completed runs in {label}")
        groups[label] = runs
    images = []
    if args.images:
        for path in sorted(args.images.glob("*/run.json")):
            raw = read_json(path)
            images.append({"run_id": raw["run_id"], "target": raw["target"],
                "measurements": [{key: sample[key] for key in ("cache", "status", "elapsed_seconds")}
                                 for sample in raw["measurements"]],
                "raw_input_sha256": digest(path)})
    evidence = {"schema_version": 1, "groups": groups, "images": images}
    summary, resources = derive(evidence)
    metadata = {"schema_version": 1, "project": args.project,
        "measurement_release": "v0.1.0", "source_commit": args.source_commit,
        "source_commit_basis": "historical report; original harness did not record HEAD",
        "platform": "Windows 11 / Docker Desktop Linux containers (linux/amd64)",
        "versions": {"go": "1.27.2", "mysql": "8.4.11", "docker": "29.8.2", "compose": "5.5.1"},
        "workload_lock": read_json(pathlib.Path(__file__).parent / "lock.json"),
        "privacy": "allowlisted numeric samples and run fields; no tokens, DSNs, ports, container IDs or logs",
        "limits": "historical short single-host regression workload, not saturation, soak or v0.1.1 capacity"}
    args.output.mkdir(parents=True)
    for name, data in zip(FILES, (evidence, summary, resources, metadata)):
        write_json(args.output / name, data)
    (args.output / "checksums.txt").write_text(
        "".join(f"{digest(args.output / name)}  {name}\n" for name in FILES), encoding="ascii", newline="\n")
    audit(args.output)


def audit(root):
    actual = sorted(path.name for path in root.iterdir())
    if actual != sorted((*FILES, "checksums.txt")):
        raise ValueError("unexpected evidence file set")
    lines = (root / "checksums.txt").read_text().splitlines()
    if len(lines) != len(FILES):
        raise ValueError("invalid evidence checksum count")
    checked = set()
    for line in lines:
        match = re.fullmatch(r"([a-f0-9]{64})  ([a-z.]+)", line)
        if not match or match[2] not in FILES or match[2] in checked:
            raise ValueError("invalid or duplicate evidence checksum entry")
        checked.add(match[2])
        if digest(root / match[2]) != match[1]:
            raise ValueError("evidence checksum mismatch")
    evidence = read_json(root / "evidence.json")
    summary, resources = derive(evidence)
    if summary != read_json(root / "summary.json") or resources != read_json(root / "resources.json"):
        raise ValueError("derived evidence statistics mismatch")


def pack(root, output):
    audit(root)
    if output.exists():
        raise ValueError("archive already exists")
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w", format=tarfile.USTAR_FORMAT) as archive:
        for name in sorted((*FILES, "checksums.txt")):
            data = (root / name).read_bytes()
            entry = tarfile.TarInfo("benchmark-evidence/" + name)
            entry.size, entry.mode, entry.mtime = len(data), 0o644, 0
            archive.addfile(entry, io.BytesIO(data))
    output.parent.mkdir(parents=True, exist_ok=True)
    with output.open("wb") as stream:
        with gzip.GzipFile(fileobj=stream, mode="wb", filename="", mtime=0) as compressed:
            compressed.write(buffer.getvalue())
    output.with_name(output.name + ".sha256").write_text(
        f"{digest(output)}  {output.name}\n", encoding="ascii", newline="\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    exporter = commands.add_parser("export")
    exporter.add_argument("--normal", type=pathlib.Path, required=True)
    exporter.add_argument("--recovery", type=pathlib.Path, required=True)
    exporter.add_argument("--images", type=pathlib.Path)
    exporter.add_argument("--source-commit", required=True)
    exporter.add_argument("--project", choices=("event-workflow-platform", "cloud-security-platform"), required=True)
    exporter.add_argument("--output", type=pathlib.Path, required=True)
    checker = commands.add_parser("audit")
    checker.add_argument("root", type=pathlib.Path)
    packer = commands.add_parser("pack")
    packer.add_argument("root", type=pathlib.Path)
    packer.add_argument("output", type=pathlib.Path)
    args = parser.parse_args()
    if args.command == "export":
        export(args)
    elif args.command == "audit":
        audit(args.root)
    else:
        pack(args.root, args.output)


if __name__ == "__main__":
    main()
