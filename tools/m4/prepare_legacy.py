"""Verify a private snapshot and copy matching local TXT into a new rehearsal root."""

import argparse
import codecs
from contextlib import closing
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import tarfile

from capture_legacy import fingerprint


def browser_gb18030_error(error):
    # WHATWG's standalone 0x80 is the euro sign; Python's codec rejects it.
    # Only that exact invalid byte is accepted. Broken pairs/quads still fail.
    if isinstance(error, UnicodeDecodeError) and error.object[error.start:error.end] == b"\x80":
        return "\u20ac", error.end
    raise error


codecs.register_error("browser_gb18030", browser_gb18030_error)


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def contained(root, name):
    relative = PurePosixPath(name)
    if relative.is_absolute() or ".." in relative.parts or ":" in name or "\\" in name:
        raise ValueError("Unsafe relative path")
    target = root.joinpath(*relative.parts)
    if not target.resolve().is_relative_to(root.resolve()):
        raise ValueError("Path escapes root")
    return target


def unpack(archive_path, output):
    output.mkdir(parents=True, exist_ok=False)
    with tarfile.open(archive_path, mode="r:gz") as archive:
        for entry in archive:
            target = contained(output, entry.name)
            if entry.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif entry.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                with target.open("xb") as destination, archive.extractfile(entry) as source:
                    shutil.copyfileobj(source, destination, 1024 * 1024)
            else:
                raise ValueError("Archive links and special files are not allowed")


def copy_text(source, destination):
    """Validate BOM-identified UTF-16, UTF-8 or browser-compatible GB18030."""
    before = source.stat()
    digest = hashlib.sha256()
    decoder = codecs.getincrementaldecoder("utf-8")("strict")
    utf8 = True
    destination.parent.mkdir(parents=True, exist_ok=True)
    with source.open("rb") as stream, destination.open("xb") as copied:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
            copied.write(chunk)
            if utf8:
                try:
                    decoder.decode(chunk)
                except UnicodeDecodeError:
                    utf8 = False
        if utf8:
            try:
                decoder.decode(b"", final=True)
            except UnicodeDecodeError:
                utf8 = False
    after = source.stat()
    if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
        raise ValueError("Local source changed during copy")
    encoding = "utf-8"
    if not utf8:
        with destination.open("rb") as stream:
            prefix = stream.read(4)
            if prefix.startswith((b"\xff\xfe\x00\x00", b"\x00\x00\xfe\xff")):
                raise ValueError("UTF-32 is not supported")
            encoding = "utf-16le" if prefix.startswith(b"\xff\xfe") else "utf-16be" if prefix.startswith(b"\xfe\xff") else "gb18030"
            decoder = codecs.getincrementaldecoder(encoding)("browser_gb18030" if encoding == "gb18030" else "strict")
            stream.seek(0)
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                decoder.decode(chunk)
            decoder.decode(b"", final=True)
    # Persist the copied file's actual platform timestamp in the manifest.
    shutil.copystat(source, destination)
    copied = destination.stat()
    return {"encoding": encoding, "size": copied.st_size, "modifiedNs": str(copied.st_mtime_ns)}, digest.hexdigest()


def prepare(snapshot, local_novels, output, supplement=None, extra_novels=()):
    output.mkdir(parents=True, exist_ok=False)
    metadata = json.loads((snapshot / "snapshot.json").read_text(encoding="utf-8"))
    if metadata["format"] != "personal-library/legacy-snapshot@1":
        raise ValueError("Unsupported snapshot format")
    database = snapshot / "library.db"
    if fingerprint(database)["sha256"] != metadata["database"]["sha256"]:
        raise ValueError("Database fingerprint mismatch")
    remote_files = {file["path"]: file for file in metadata["novels"]}
    if supplement:
        extra = json.loads(supplement.read_text(encoding="utf-8"))
        for file in extra["novels"]:
            if file["path"] in remote_files:
                raise ValueError("Duplicate supplemental path")
            remote_files[file["path"]] = file
    report = {"complete": False, "at": datetime.now(timezone.utc).isoformat(), "sourceVersion": metadata["sourceVersion"],
              "sourceSha256": metadata["database"]["sha256"], "counts": metadata["tables"],
              "supplement": str(supplement) if supplement else None, "files": [], "issues": [],
              "novelRoots": [str(root) for root in (local_novels, *extra_novels)]}
    try:
        with closing(sqlite3.connect(database.resolve().as_uri() + "?mode=ro", uri=True)) as db:
            rows = db.execute("SELECT id,filePath FROM books WHERE hasContent=1 ORDER BY id").fetchall()
        references = {}
        for ident, name in rows:
            references.setdefault(name.replace("\\", "/"), []).append(ident)
        report["readableBooks"] = len(rows)
        report["distinctPaths"] = len(references)
        report["sharedFiles"] = [{"path": name, "bookIds": ids} for name, ids in references.items() if len(ids) > 1]
        report["unreferencedFiles"] = sorted(remote_files.keys() - references.keys())
        manifest = []
        for index, name in enumerate(sorted(references), 1):
            if name not in remote_files:
                report["issues"].append({"path": name, "error": "missing_remote_fingerprint"})
                continue
            expected = remote_files[name]
            source = contained(local_novels, name)
            if not source.is_file():
                source = next((p for root in extra_novels if (p := contained(root, name)).is_file()), source)
            if not source.is_file() or source.stat().st_size != expected["size"]:
                report["issues"].append({"path": name, "error": "local_file_missing_or_size_differs"})
                continue
            target = contained(output / "novels", name)
            try:
                observed, digest = copy_text(source, target)
                if digest != expected["sha256"]:
                    raise ValueError("Local and remote file fingerprints differ")
                manifest.append({"path": name, **observed})
                report["files"].append({"path": name, "sha256": digest, "encoding": observed["encoding"], "size": observed["size"]})
            except (ValueError, UnicodeError) as error:
                report["issues"].append({"path": name, "error": str(error)})
            if index % 250 == 0:
                print(f"Validated and copied {index}/{len(references)} TXT references", flush=True)
        for file in metadata["covers"]:
            source = contained(snapshot / "covers", file["path"])
            if fingerprint(source)["sha256"] != file["sha256"]:
                raise ValueError("Cover fingerprint mismatch")
        report["verifiedCovers"] = len(metadata["covers"])
        if report["issues"]:
            raise ValueError(f"Preparation found {len(report['issues'])} issues; see preparation.json")
        write_json(output / "manifest.json", {"version": 1, "files": manifest})
        report["complete"] = True
        return report
    finally:
        write_json(output / "preparation.json", report)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--local-novels", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--supplement", type=Path)
    parser.add_argument("--extra-novels", type=Path, action="append", default=[], help="additional local roots for missing TXT files")
    args = parser.parse_args()
    root = args.out.resolve()
    root.mkdir(parents=True, exist_ok=False)
    unpack(args.archive, root / "snapshot")
    result = prepare(root / "snapshot", args.local_novels.resolve(), root / "prepared", args.supplement, args.extra_novels)
    print(json.dumps({"complete": result["complete"], "readableBooks": result["readableBooks"],
                      "files": result["distinctPaths"], "covers": result["verifiedCovers"]}))
