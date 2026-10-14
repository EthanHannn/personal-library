from contextlib import closing
import io
import json
from pathlib import Path
import sqlite3
import tarfile
import tempfile
import unittest

from capture_legacy import TABLES, capture


class CaptureTests(unittest.TestCase):
    def test_online_backup_keeps_committed_wal_and_restores(self):
        with tempfile.TemporaryDirectory(prefix="library-capture-test-") as temp:
            root = Path(temp)
            (root / "novels").mkdir()
            (root / "covers").mkdir()
            (root / "novels/one.txt").write_text("第一章\n快照样本。", encoding="utf-8")
            (root / "novels/upper.TXT").write_text("大写扩展名。", encoding="utf-8")
            (root / "covers/one.svg").write_text("<svg/>", encoding="utf-8")
            with closing(sqlite3.connect(root / "source.db")) as writer:
                writer.execute("PRAGMA journal_mode=WAL")
                for table in TABLES:
                    writer.execute("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, hasContent INTEGER)")
                writer.execute("INSERT INTO books VALUES(1,1)")
                writer.commit()
                writer.execute("INSERT INTO books VALUES(2,1)")
                buffer = io.BytesIO()
                report = capture(root / "source.db", root / "novels", root / "covers", buffer, version="synthetic")
                self.assertEqual(report["tables"]["books"], 1)
                self.assertEqual({item["path"] for item in report["novels"]}, {"one.txt", "upper.TXT"})
                with tarfile.open(fileobj=io.BytesIO(buffer.getvalue()), mode="r:gz") as archive:
                    self.assertEqual(json.load(archive.extractfile("snapshot.json"))["tables"]["books"], 1)
                    self.assertEqual(archive.extractfile("covers/one.svg").read(), b"<svg/>")
                    (root / "restored.db").write_bytes(archive.extractfile("library.db").read())
                with closing(sqlite3.connect(root / "restored.db")) as restored:
                    self.assertEqual(restored.execute("SELECT count(*) FROM books").fetchone()[0], 1)
                    self.assertEqual(restored.execute("PRAGMA integrity_check").fetchone()[0], "ok")
                self.assertEqual(writer.execute("SELECT count(*) FROM books").fetchone()[0], 2)
                writer.rollback()

    def test_credentials_are_not_exported(self):
        with tempfile.TemporaryDirectory(prefix="library-capture-test-") as temp:
            root = Path(temp)
            with closing(sqlite3.connect(root / "source.db")) as source:
                source.execute("CREATE TABLE users(id TEXT, password TEXT)")
            output = io.BytesIO()
            with self.assertRaisesRegex(RuntimeError, "credential columns"):
                capture(root / "source.db", root / "novels", root / "covers", output)
            self.assertEqual(output.getvalue(), b"")


if __name__ == "__main__":
    unittest.main()
