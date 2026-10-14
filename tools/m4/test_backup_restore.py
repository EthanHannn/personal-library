from contextlib import closing
import io
from pathlib import Path
import sqlite3
import tarfile
import tempfile
import unittest

from backup_restore import backup, restore
from prepare_legacy import contained, unpack


class BackupTests(unittest.TestCase):
    def test_restore_preserves_committed_wal_and_assets(self):
        with tempfile.TemporaryDirectory(prefix="library-backup-test-") as temp:
            root = Path(temp)
            source = root / "live"
            (source / "covers").mkdir(parents=True)
            (source / "collected").mkdir()
            (source / "covers/image.png").write_bytes(b"synthetic-image")
            (source / "collected/book.txt").write_text("新采集正文", encoding="utf-8")
            with closing(sqlite3.connect(source / "library.db")) as writer:
                writer.execute("PRAGMA journal_mode=WAL")
                writer.execute("CREATE TABLE progress(id INTEGER PRIMARY KEY, chapter INTEGER)")
                writer.execute("INSERT INTO progress VALUES(1,25)")
                writer.commit()
                writer.execute("INSERT INTO progress VALUES(2,99)")
                backup(source, root / "backup")
                restore(root / "backup", root / "restored")
                with closing(sqlite3.connect(root / "restored/library.db")) as result:
                    self.assertEqual(result.execute("SELECT * FROM progress").fetchall(), [(1, 25)])
                    self.assertEqual(result.execute("PRAGMA journal_mode").fetchone()[0], "wal")
                self.assertEqual((root / "restored/collected/book.txt").read_text(encoding="utf-8"), "新采集正文")
                self.assertEqual((root / "restored/covers/image.png").read_bytes(), b"synthetic-image")
                with self.assertRaises(FileExistsError):
                    restore(root / "backup", root / "restored")
                (root / "backup/covers/image.png").write_bytes(b"damaged")
                with self.assertRaisesRegex(ValueError, "fingerprint mismatch"):
                    restore(root / "backup", root / "rejected")
                self.assertFalse((root / "rejected").exists())
                writer.rollback()

    def test_paths_and_archives_cannot_escape(self):
        with tempfile.TemporaryDirectory(prefix="library-path-test-") as temp:
            root = Path(temp)
            for value in ("../escape", "/absolute", "C:/escape", "a\\..\\escape"):
                with self.assertRaises(ValueError):
                    contained(root, value)
            archive_path = root / "bad.tar.gz"
            with tarfile.open(archive_path, "w:gz") as archive:
                entry = tarfile.TarInfo("../escape")
                entry.size = 4
                archive.addfile(entry, io.BytesIO(b"test"))
            with self.assertRaises(ValueError):
                unpack(archive_path, root / "output")
            self.assertFalse((root / "escape").exists())


if __name__ == "__main__":
    unittest.main()
