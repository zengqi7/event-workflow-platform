import json
import pathlib
import tempfile
import unittest

from resources import aggregate, memory_mib, read_samples


class ResourceTests(unittest.TestCase):
    def samples(self, values):
        return [{"role": "api", "cpu_percent": value, "memory_mib": value} for value in values]

    def test_135_samples_use_68th_value(self):
        result = aggregate(self.samples(range(1, 136)))["api"]
        self.assertEqual(result["cpu_median_percent"], 68)
        self.assertEqual(result["memory_median_mib"], 68)
        self.assertEqual(result["samples"], 135)

    def test_even_samples_average_middle(self):
        self.assertEqual(aggregate(self.samples([1, 4, 2, 3]))["api"]["cpu_median_percent"], 2.5)

    def test_units(self):
        self.assertEqual(memory_mib("1.5GiB / 2GiB"), 1536)
        self.assertEqual(memory_mib("1024KiB / 2MiB"), 1)
        self.assertAlmostEqual(memory_mib("1MB / 2GB"), 1000000 / 1024**2)
        self.assertEqual(memory_mib("0B / 512MiB"), 0)

    def test_reject_invalid_values(self):
        for text in ["nanMiB", "1XB", "-1MiB"]:
            with self.assertRaises(ValueError):
                memory_mib(text)
        for value in [float("nan"), float("inf"), -1]:
            with self.assertRaises(ValueError):
                aggregate(self.samples([value]))
        with self.assertRaises(ValueError):
            aggregate([])

    def test_normalize_and_remove_container_identity(self):
        with tempfile.TemporaryDirectory() as folder:
            path = pathlib.Path(folder) / "samples.jsonl"
            path.write_text(json.dumps({"timestamp": "2026-10-11T00:00:00Z",
                "container": {"Name": "event-run-worker-2", "CPUPerc": "100.74%",
                              "MemUsage": "12MiB / 512MiB", "ID": "private"}}), encoding="utf-8")
            sample = read_samples(path)[0]
            self.assertEqual(sample["role"], "worker")
            self.assertEqual(sample["cpu_percent"], 100.74)
            self.assertNotIn("ID", sample)


if __name__ == "__main__":
    unittest.main()
