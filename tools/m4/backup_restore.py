"""Back up or restore an isolated library data directory without replacing files.

Original read-only novels are referenced separately; covers and collected files
are copied with hashes. Quiesce background content jobs while making a bundle.
SQLite itself uses its online backup API, including committed WAL pages.
"""

import argparse
from contextlib import closing
from datetime import datetime, timezone
import json
from pathlib import Path
import shutil
import sqlite3

from capture_legacy import fingerprint
from prepare_legacy import contained, write_json


def inspect(database):
    with closing(sqlite3.connect(database.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        integrity = [row[0] for row in db.execute("PRAGMA integrity_check")]
        if integrity != ["ok"] or db.execute("PRAGMA foreign_key_check").fetchone():
            raise ValueError("Database integrity or foreign key check failed")
        names = [row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")]
        counts = {name: db.execute('SELECT count(*) FROM "' + name.replace('"', '""') + '"').fetchone()[0] for name in names}
        return {"integrity": "ok", "tables": counts}


def backup(source, output):
    source, output = source.resolve(), output.resolve()
    if output.is_relative_to(source):
        raise ValueError("Backup must be outside its input data directory")
    output.mkdir(parents=True, exist_ok=False)
    database = source / "library.db"
    with closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as src:
        with closing(sqlite3.connect(output / "library.db")) as dest:
            src.backup(dest, pages=512, sleep=0.05)
            dest.execute("PRAGMA journal_mode=DELETE")
    report = {"format": "personal-library/data-backup@1", "createdAt": datetime.now(timezone.utc).isoformat(),
              "database": inspect(output / "library.db"), "files": [], "originalNovelsIncluded": False}
    for folder in ("covers", "collected"):
        origin = source / folder
        (output / folder).mkdir()
        if not origin.exists():
            continue
        if origin.is_symlink():
            raise ValueError("Asset root must not be a symlink")
        for path in sorted(origin.rglob("*")):
            if path.is_symlink():
                raise ValueError("Asset links are not backed up")
            if not path.is_file():
                continue
            name = path.relative_to(source).as_posix()
            target = contained(output, name)
            before = fingerprint(path)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, target)
            after = fingerprint(target)
            if before != fingerprint(path) or before["sha256"] != after["sha256"]:
                raise ValueError("Asset changed while backing up; retry after jobs stop")
    for path in sorted(output.rglob("*")):
        if path.is_file():
            report["files"].append({"path": path.relative_to(output).as_posix(), **fingerprint(path)})
    write_json(output / "backup.json", report)
    return report


def restore(source, output):
    source, output = source.resolve(), output.resolve()
    if output.is_relative_to(source):
        raise ValueError("Restore must be outside its input backup directory")
    report = json.loads((source / "backup.json").read_text(encoding="utf-8"))
    if report["format"] != "personal-library/data-backup@1":
        raise ValueError("Unsupported backup format")
    names = [file["path"] for file in report["files"]]
    if len(set(names)) != len(names) or "library.db" not in names:
        raise ValueError("Missing database or duplicate backup path")
    # Validate everything before writing a restore destination.
    for file in report["files"]:
        path = contained(source, file["path"])
        if path.is_symlink() or fingerprint(path)["sha256"] != file["sha256"]:
            raise ValueError("Backup fingerprint mismatch")
    output.mkdir(parents=True, exist_ok=False)
    for file in report["files"]:
        target = contained(output, file["path"])
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(contained(source, file["path"]), target)
        if fingerprint(target)["sha256"] != file["sha256"]:
            raise ValueError("Restored fingerprint mismatch")
    for folder in ("covers", "collected"):
        (output / folder).mkdir(exist_ok=True)
    observed = inspect(output / "library.db")
    if observed != report["database"]:
        raise ValueError("Restored database differs from backup")
    write_json(output / "restore.json", {"complete": True, "at": datetime.now(timezone.utc).isoformat(),
                                        "verifiedFiles": len(names), "database": observed})
    return observed


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("backup", "restore"))
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    result = (backup if args.operation == "backup" else restore)(args.source, args.out)
    print(json.dumps({"complete": True, "operation": args.operation, "database": result.get("database", result)}))
