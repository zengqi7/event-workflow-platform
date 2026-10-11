#!/usr/bin/env python3
import argparse
import hashlib
import io
import json
import pathlib
import tarfile


ROOT = pathlib.Path(__file__).resolve().parents[1]
LOCK = json.loads((ROOT / "capacity" / "lock.json").read_text(encoding="utf-8"))


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description="Prepare the pinned webhook capacity corpus")
    parser.add_argument("archive", type=pathlib.Path)
    parser.add_argument("--output", type=pathlib.Path, default=ROOT / "data" / "capacity")
    args = parser.parse_args()

    archive_bytes = args.archive.read_bytes()
    actual = sha256(archive_bytes)
    expected = LOCK["source"]["archive_sha256"]
    if actual != expected:
        raise SystemExit(f"archive checksum mismatch: expected {expected}, got {actual}")

    selected = []
    license_bytes = None
    categories = set(LOCK["selection"]["event_types"])
    with tarfile.open(fileobj=io.BytesIO(archive_bytes), mode="r:gz") as archive:
        for member in archive.getmembers():
            parts = pathlib.PurePosixPath(member.name).parts
            if member.isfile() and parts[-1] == "LICENSE":
                license_bytes = archive.extractfile(member).read()
            if not member.isfile() or "payload-examples" not in parts:
                continue
            try:
                marker = parts.index("api.github.com")
                category = parts[marker + 1]
            except (ValueError, IndexError):
                continue
            filename = parts[-1]
            if category not in categories or not filename.endswith(LOCK["selection"]["include_suffix"]):
                continue
            raw = archive.extractfile(member).read()
            payload = json.loads(raw)
            selected.append({
                "source_path": "/".join(parts[marker:]),
                "source_sha256": sha256(raw),
                "source_event_type": category,
                "payload": payload,
            })

    selected.sort(key=lambda item: item["source_path"])
    if len(selected) != LOCK["expected_payloads"]:
        raise SystemExit(f"selection drift: expected {LOCK['expected_payloads']}, got {len(selected)}")
    if license_bytes is None:
        raise SystemExit("upstream LICENSE not found")

    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    corpus_bytes = (json.dumps(selected, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n").encode()
    (output / "corpus.json").write_bytes(corpus_bytes)
    (output / "upstream-LICENSE").write_bytes(license_bytes)
    manifest = {
        "source_commit": LOCK["source"]["commit"],
        "source_archive_sha256": actual,
        "selection_count": len(selected),
        "corpus_sha256": sha256(corpus_bytes),
        "files": [{"path": item["source_path"], "sha256": item["source_sha256"]} for item in selected],
    }
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(output), "count": len(selected), "corpus_sha256": manifest["corpus_sha256"]}))


if __name__ == "__main__":
    main()
