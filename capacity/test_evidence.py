import hashlib
import json
import pathlib
import tempfile
import unittest

from evidence import audit, export_run, pack


class EvidenceTests(unittest.TestCase):
    def test_export_allowlist_and_queue_validation(self):
        with tempfile.TemporaryDirectory() as folder:
            root = pathlib.Path(folder)
            (root / "run.json").write_text(json.dumps({"run_id": "event-test-1",
                "completed_count": 2, "http_port": 9999, "secret": "do-not-export"}))
            (root / "k6-summary.json").write_text(json.dumps({
                "state": {"testRunDurationMs": 1000},
                "metrics": {"iterations": {"values": {"count": 2}},
                            "accepted_latency": {"values": {"p(95)": 3}}},
                "secret": "do-not-export"}))
            (root / "completion-latency-ms.txt").write_text("1\n2\n")
            (root / "queue-samples.jsonl").write_text(json.dumps({
                "timestamp": "2026-10-11T00:00:00Z", "terminal": 2, "total": 2, "secret": "do-not-export"}))
            (root / "resource-samples.jsonl").write_text(json.dumps({
                "timestamp": "2026-10-11T00:00:00Z",
                "container": {"Name": "event-test-api-1", "CPUPerc": "1%",
                              "MemUsage": "2MiB / 512MiB", "ID": "do-not-export"}}))
            result = export_run(root, "a" * 40)
            self.assertNotIn("do-not-export", json.dumps(result))
            self.assertNotIn("http_port", result["run"])
            self.assertEqual(result["source_commit"], "a" * 40)
            (root / "queue-samples.jsonl").write_text(json.dumps({
                "timestamp": "2026-10-11T00:00:00Z", "terminal": 1, "total": 2}))
            with self.assertRaises(ValueError):
                export_run(root, "a" * 40)

    def test_real_evidence_and_deterministic_package(self):
        root = pathlib.Path(__file__).parent / "evidence" / "v0.1.0"
        audit(root)
        with tempfile.TemporaryDirectory() as folder:
            first, second = pathlib.Path(folder) / "a.tar.gz", pathlib.Path(folder) / "b.tar.gz"
            pack(root, first)
            pack(root, second)
            self.assertEqual(hashlib.sha256(first.read_bytes()).digest(),
                             hashlib.sha256(second.read_bytes()).digest())

    def test_tampering_is_rejected(self):
        import shutil
        root = pathlib.Path(__file__).parent / "evidence" / "v0.1.0"
        with tempfile.TemporaryDirectory() as folder:
            copied = pathlib.Path(folder) / "copy"
            shutil.copytree(root, copied)
            (copied / "resources.json").write_text("{}")
            with self.assertRaises(ValueError):
                audit(copied)


if __name__ == "__main__":
    unittest.main()
