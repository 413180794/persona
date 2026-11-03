"""redact_transcript.py runs on macOS's own Python 3.9 (#579): its datetime.fromisoformat takes only 3 or 6 fraction
digits, while Go writes RFC 3339 times with 0 to 9 (trailing zeros dropped)."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("redact_transcript", Path(__file__).with_name("redact_transcript.py"))
redact = importlib.util.module_from_spec(spec)
spec.loader.exec_module(redact)


class TimestampTests(unittest.TestCase):
    def test_any_number_of_fraction_digits(self):
        r = redact.Redactor()
        self.assertEqual(r.ts("2026-03-01T10:00:01Z"), "2026-01-01T00:00:00.000Z")  # the first time moves to 2026-01-01
        for stamp, shifted in [("2026-03-01T10:00:01.5Z", "2026-01-01T00:00:00.500Z"),
                               ("2026-03-01T10:00:01.12Z", "2026-01-01T00:00:00.120Z"),
                               ("2026-03-01T10:00:01.1234Z", "2026-01-01T00:00:00.123Z"),
                               ("2026-03-01T10:00:01.123456789Z", "2026-01-01T00:00:00.123Z")]:
            with self.subTest(stamp=stamp):
                self.assertEqual(r.ts(stamp), shifted)


if __name__ == "__main__":
    unittest.main()
