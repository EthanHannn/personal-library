from contextlib import closing
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from capture_legacy import fingerprint
from prepare_legacy import copy_text, prepare, write_json


class PrepareTests(unittest.TestCase):
    def test_utf16_and_browser_euro_preserve_bytes(self):
        with tempfile.TemporaryDirectory(prefix="library-encoding-test-") as temp:
            root = Path(temp)
            for index, (raw, encoding) in enumerate([
                (b"\xff\xfe" + "第一章😀".encode("utf-16le"), "utf-16le"),
                (b"\xfe\xff" + "第一章😀".encode("utf-16be"), "utf-16be"),
                ("第一章".encode("gb18030") + b"\x80", "gb18030"),
            ]):
                source, dest = root / f"{index}.txt", root / f"{index}-copy.txt"
                source.write_bytes(raw)
                info, _ = copy_text(source, dest)
                self.assertEqual(info["encoding"], encoding)
                self.assertEqual(dest.read_bytes(), raw)
            for index, raw in enumerate([b"\x81\x30\x80", b"\xff\xfe\x00\xd8", b"\xfe\xff\xdc\x00"]):
                source, dest = root / f"bad-{index}.txt", root / f"bad-{index}-copy.txt"
                source.write_bytes(raw)
                with self.assertRaises(UnicodeDecodeError):
                    copy_text(source, dest)

    def test_shared_files_supplement_and_full_encoding_validation(self):
        with tempfile.TemporaryDirectory(prefix="library-prepare-test-") as temp:
            root = Path(temp)
            snapshot, novels = root / "snapshot", root / "novels"
            snapshot.mkdir()
            novels.mkdir()
            (snapshot / "covers").mkdir()
            # Put the GB18030 character after the legacy 8 KiB detection window.
            (novels / "upper.TXT").write_bytes(b"a" * 9000 + "章节".encode("gb18030"))
            (snapshot / "covers/cover.png").write_bytes(b"cover-bytes")
            with closing(sqlite3.connect(snapshot / "library.db")) as db:
                db.execute("CREATE TABLE books(id INTEGER, filePath TEXT, hasContent INTEGER)")
                db.executemany("INSERT INTO books VALUES(?, 'upper.TXT', 1)", [(1,), (2,)])
                db.commit()
            write_json(snapshot / "snapshot.json", {
                "format": "personal-library/legacy-snapshot@1", "sourceVersion": "synthetic",
                "database": fingerprint(snapshot / "library.db"), "tables": {"books": 2}, "novels": [],
                "covers": [{"path": "cover.png", **fingerprint(snapshot / "covers/cover.png")}],
            })
            supplement = root / "extra.json"
            write_json(supplement, {"novels": [{"path": "upper.TXT", **fingerprint(novels / "upper.TXT")}]})
            result = prepare(snapshot, novels, root / "prepared", supplement)
            self.assertTrue(result["complete"])
            self.assertEqual(result["sharedFiles"], [{"path": "upper.TXT", "bookIds": [1, 2]}])
            manifest = json.loads((root / "prepared/manifest.json").read_text())
            self.assertEqual(manifest["files"][0]["encoding"], "gb18030")
            self.assertEqual((root / "prepared/novels/upper.TXT").read_bytes(), (novels / "upper.TXT").read_bytes())

            # A same-size changed source must not produce an importable manifest.
            (novels / "upper.TXT").write_bytes(b"b" * 9000 + "章节".encode("gb18030"))
            with self.assertRaisesRegex(ValueError, "Preparation found"):
                prepare(snapshot, novels, root / "rejected", supplement)
            self.assertFalse((root / "rejected/manifest.json").exists())
            rejected = json.loads((root / "rejected/preparation.json").read_text())
            self.assertFalse(rejected["complete"])
            self.assertIn("fingerprints differ", rejected["issues"][0]["error"])


if __name__ == "__main__":
    unittest.main()
