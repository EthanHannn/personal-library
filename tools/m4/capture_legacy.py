"""Create an isolated legacy SQLite backup and stream a private rehearsal archive.

The production entry point takes no arguments and is embedded in the approved
aliyun-ecs-ops.sh library-snapshot command. It never reads portal credentials,
changes the original database, or restarts services. TXT bodies stay in place;
their fingerprints let the workstation reuse identical local files.
"""

from datetime import datetime, timezone
from contextlib import closing
import gzip
import hashlib
import io
import json
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import time

TABLES = ("books", "chapters", "users", "bookshelf_entries", "reading_progress", "votes", "comments")


def fingerprint(path):
    before = path.stat()
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    after = path.stat()
    if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
        raise RuntimeError("Source file changed during fingerprinting")
    return {"size": after.st_size, "modifiedNs": str(after.st_mtime_ns), "sha256": digest.hexdigest()}


def capture(source, novels, covers, sink, *, version="unknown"):
    started = time.monotonic()
    metadata = {"format": "personal-library/legacy-snapshot@1", "capturedAt": datetime.now(timezone.utc).isoformat(),
                "sourceVersion": version, "sqliteVersion": sqlite3.sqlite_version, "novels": [], "covers": []}
    with tempfile.TemporaryDirectory(prefix="library-snapshot-") as temporary:
        root = Path(temporary)
        backup = root / "library.db"
        with closing(sqlite3.connect(source.resolve().as_uri() + "?mode=ro", uri=True, timeout=30)) as original:
            columns = {row[1].lower() for row in original.execute("PRAGMA table_info(users)")}
            if any("password" in column or "secret" in column or "token" in column for column in columns):
                raise RuntimeError("Unexpected legacy credential columns; snapshot refused")
            with closing(sqlite3.connect(backup)) as destination:
                original.execute("PRAGMA cache_size=-2048")
                original.backup(destination, pages=512, sleep=0.05,
                                progress=lambda *_: time.sleep(0.005))
                destination.execute("PRAGMA journal_mode=DELETE")
                metadata["integrity"] = destination.execute("PRAGMA quick_check").fetchone()[0]
                if metadata["integrity"] != "ok":
                    raise RuntimeError("Snapshot integrity check failed")
                metadata["tables"] = {name: destination.execute("SELECT count(*) FROM " + name).fetchone()[0] for name in TABLES}
                metadata["readableBooks"] = destination.execute("SELECT count(*) FROM books WHERE hasContent=1").fetchone()[0]
        metadata["database"] = fingerprint(backup)
        print("SQLite consistent snapshot complete; fingerprinting TXT files", file=sys.stderr, flush=True)
        text_files = (path for path in novels.rglob("*") if path.is_file() and path.suffix.lower() == ".txt")
        for index, path in enumerate(sorted(text_files), 1):
            if path.is_symlink() or not path.resolve().is_relative_to(novels.resolve()):
                raise RuntimeError("Unexpected novel symlink or path outside root")
            metadata["novels"].append({"path": path.relative_to(novels).as_posix(), **fingerprint(path)})
            if index % 250 == 0:
                print(f"Fingerprinted {index} TXT files", file=sys.stderr, flush=True)
        (root / "covers").mkdir()
        for path in sorted(covers.rglob("*")):
            if path.is_symlink():
                raise RuntimeError("Unexpected cover symlink")
            if not path.is_file():
                continue
            if not path.resolve().is_relative_to(covers.resolve()):
                raise RuntimeError("Cover path outside root")
            relative = path.relative_to(covers)
            copied = root / "covers" / relative
            copied.parent.mkdir(parents=True, exist_ok=True)
            before = fingerprint(path)
            shutil.copy2(path, copied)
            copied_meta = fingerprint(copied)
            if copied_meta["sha256"] != before["sha256"] or fingerprint(path) != before:
                raise RuntimeError("Cover changed during snapshot")
            metadata["covers"].append({"path": relative.as_posix(), **copied_meta})
        metadata["preparationSeconds"] = round(time.monotonic() - started, 3)
        manifest = json.dumps(metadata, ensure_ascii=False, indent=2).encode("utf-8")
        print("Streaming database, covers and fingerprints to workstation", file=sys.stderr, flush=True)
        with gzip.GzipFile(fileobj=sink, mode="wb", compresslevel=6, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w|") as archive:
                info = tarfile.TarInfo("snapshot.json")
                info.size = len(manifest)
                info.mode = 0o600
                archive.addfile(info, io.BytesIO(manifest))
                archive.add(backup, arcname="library.db")
                archive.add(root / "covers", arcname="covers")
    return metadata


if __name__ == "__main__":
    if len(sys.argv) != 1:
        raise SystemExit("This production collector accepts no arguments")
    app = Path("/opt/zhixuan-library")
    revision = subprocess.run(["docker", "inspect", "--format", '{{index .Config.Labels "org.opencontainers.image.revision"}}',
                               "zhixuan-library"], capture_output=True, text=True, check=True).stdout.strip()
    capture(app / "data/library.db", app / "novels", app / "covers", sys.stdout.buffer, version=revision)
