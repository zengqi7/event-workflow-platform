#!/usr/bin/env python3
"""Aggregate Docker resource samples from completed capacity runs."""
import argparse
import hashlib
import json
import math
import pathlib
import re
import statistics

UNITS = {"B": 1, "kB": 1000, "KB": 1000, "MB": 1000**2,
         "GB": 1000**3, "KiB": 1024, "MiB": 1024**2, "GiB": 1024**3}


def memory_mib(text):
    match = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*([A-Za-z]+)\s*", text.split("/")[0])
    if not match or match[2] not in UNITS:
        raise ValueError(f"invalid Docker memory value: {text!r}")
    return float(match[1]) * UNITS[match[2]] / 1024**2


def finite_number(value):
    number = float(value)
    if not math.isfinite(number) or number < 0:
        raise ValueError("resource values must be finite and nonnegative")
    return number


def read_samples(path):
    samples = []
    for line in path.read_text(encoding="utf-8-sig").splitlines():
        raw = json.loads(line)
        container = raw["container"]
        match = re.search(r"-(api|worker|mysql|redis|webhook-sink)-\d+$", container["Name"])
        if not match:
            raise ValueError("unrecognized capacity container role")
        cpu = container["CPUPerc"]
        if not re.fullmatch(r"\d+(?:\.\d+)?%", cpu):
            raise ValueError("invalid Docker CPU percentage")
        samples.append({"role": match[1], "timestamp": raw["timestamp"],
                        "phase": raw.get("phase", "load"),
                        "cpu_percent": finite_number(cpu[:-1]),
                        "memory_mib": finite_number(memory_mib(container["MemUsage"]))})
    if not samples:
        raise ValueError("empty resource samples")
    return samples


def aggregate(samples):
    result = {}
    for role in sorted({sample["role"] for sample in samples}):
        selected = [sample for sample in samples if sample["role"] == role]
        cpu = [finite_number(sample["cpu_percent"]) for sample in selected]
        memory = [finite_number(sample["memory_mib"]) for sample in selected]
        result[role] = {"samples": len(selected),
                        "cpu_median_percent": statistics.median(cpu),
                        "cpu_peak_percent": max(cpu),
                        "memory_median_mib": statistics.median(memory),
                        "memory_peak_mib": max(memory)}
    if not result:
        raise ValueError("no resource samples to aggregate")
    return result


def collect(root):
    samples, inputs = [], []
    for run_file in sorted(root.glob("*/run.json")):
        path = run_file.parent / "resource-samples.jsonl"
        run_samples = read_samples(path)
        samples.extend(run_samples)
        inputs.append({"run_id": run_file.parent.name,
                       "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                       "samples": len(run_samples)})
    return {"schema_version": 1, "scope": "all load and drain samples; workers per container",
            "median_method": "statistics.median (middle value or mean of two middle values)",
            "inputs": inputs, "roles": aggregate(samples)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path)
    args = parser.parse_args()
    output = json.dumps(collect(args.root), indent=2, sort_keys=True) + "\n"
    if args.output:
        args.output.write_text(output, encoding="utf-8")
    else:
        print(output, end="")


if __name__ == "__main__":
    main()
