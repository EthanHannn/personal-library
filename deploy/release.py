"""One-time legacy-to-Go production cutover, invoked by the ECS ops wrapper.

No credentials are read. Only the library upstream changes. A durable nginx
maintenance marker prevents user writes until migration, restart and restore
checks pass. Once public access opens, automatic legacy rollback is forbidden.
"""

from contextlib import closing
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import shutil
import signal
import sqlite3
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools/m4"))
from backup_restore import backup, restore
from capture_legacy import fingerprint
from prepare_legacy import contained, write_json
from verify_production import public_checks, request, verify

ROOT = Path("/opt/personal-library")
LEGACY = Path("/opt/zhixuan-library")
NGINX = Path("/etc/nginx/sites-available/personal-portal")
MARKER = ROOT / "maintenance"
ENV = {
    "PORTAL_ALLOWED_USER": "owner",
    "LIBRARY_PUBLIC_URL": "https://library.private.xuexikuaile.fun",
    "PORTAL_AUTH_URL": "https://auth.private.xuexikuaile.fun",
    "PORTAL_PUBLIC_URL": "https://portal.private.xuexikuaile.fun",
}
OLD = "zhixuan-library"
NEW = "personal-library"
RESTORED = "personal-library-restore-check"


def run(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, text=True, **kwargs)


def output(*args):
    return run(*args, capture_output=True).stdout.strip()


def log(message):
    print(datetime.now(timezone.utc).isoformat(timespec="seconds"), message, flush=True)


def verify_bundle(stage):
    package = json.loads((stage / "package.json").read_text(encoding="utf-8"))
    if package.get("format") != "personal-library/release@1" or not re.fullmatch("[0-9a-f]{40}", package.get("revision", "")):
        raise ValueError("Invalid release identity")
    if not re.fullmatch("sha256:[0-9a-f]{64}", package.get("imageId", "")):
        raise ValueError("Invalid image identity")
    files = package["files"]
    if set(files) != {p.relative_to(stage).as_posix() for p in stage.rglob("*") if p.is_file()} - {"package.json"}:
        raise ValueError("Package has missing or unexpected files")
    for name, expected in files.items():
        path = contained(stage, name)
        if path.is_symlink() or fingerprint(path)["sha256"] != expected:
            raise ValueError("Package fingerprint mismatch")
    return package


def maintenance_config(original):
    anchor = "    server_name library.private.xuexikuaile.fun;\n"
    # This production contract has exactly one HTTP and one HTTPS block. Keep
    # all portal/auth directives intact; the marker applies only to HTTPS library.
    parts = original.split("\nserver {")
    matched = 0
    for index, part in enumerate(parts):
        if anchor in part and "listen 443 ssl http2;" in part:
            if "personal-library/maintenance" in part:
                raise ValueError("A previous maintenance configuration already exists")
            parts[index] = part.replace(anchor, anchor + f"    if (-f {MARKER.as_posix()}) {{ return 503; }}\n", 1)
            matched += 1
    if matched != 1:
        raise ValueError("Unexpected nginx library server layout")
    return "\nserver {".join(parts)


def chown_data(path):
    # Never follows a symlink or touches the read-only original novel tree.
    for item in (path, *path.rglob("*")):
        if item.is_symlink():
            raise ValueError("Unexpected data symlink")
        os.chown(item, 65532, 65532)
        item.chmod(0o750 if item.is_dir() else 0o640)


def snapshot_legacy(destination):
    destination.mkdir(mode=0o755)
    with closing(sqlite3.connect((LEGACY / "data/library.db").as_uri() + "?mode=ro", uri=True)) as source:
        columns = [row[1].lower() for row in source.execute("PRAGMA table_info(users)")]
        if any(word in column for column in columns for word in ("password", "secret", "token")):
            raise ValueError("Unexpected credential columns in legacy library")
        with closing(sqlite3.connect(destination / "library.db")) as copied:
            source.backup(copied, pages=512, sleep=0.05)
            copied.execute("PRAGMA journal_mode=DELETE")
            if copied.execute("PRAGMA quick_check").fetchone()[0] != "ok":
                raise ValueError("Legacy snapshot failed quick_check")
            counts = {name: copied.execute("SELECT count(*) FROM " + name).fetchone()[0]
                      for name in ("books", "chapters", "users", "bookshelf_entries", "reading_progress", "votes", "comments")}
    (destination / "library.db").chmod(0o644)
    covers = []
    for source in sorted((LEGACY / "covers").rglob("*")):
        if source.is_symlink():
            raise ValueError("Unexpected legacy cover symlink")
        if source.is_file():
            name = source.relative_to(LEGACY / "covers").as_posix()
            target = contained(destination / "covers", name)
            target.parent.mkdir(parents=True, exist_ok=True)
            before = fingerprint(source)
            shutil.copy2(source, target)
            if fingerprint(target)["sha256"] != before["sha256"] or fingerprint(source) != before:
                raise ValueError("Legacy cover changed during snapshot")
            covers.append({"path": name, **before})
    snapshot = {"at": datetime.now(timezone.utc).isoformat(), "tables": counts,
                "database": fingerprint(destination / "library.db"), "covers": covers}
    write_json(destination / "snapshot.json", snapshot)
    return snapshot


def prepare_manifest(evidence, database, novels, destination):
    if evidence.get("complete") is not True:
        raise ValueError("Encoding evidence is incomplete")
    files = {item["path"]: item for item in evidence["files"]}
    if len(files) != len(evidence["files"]):
        raise ValueError("Duplicate encoding evidence")
    with closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as db:
        references = {row[0].replace("\\", "/") for row in db.execute("SELECT filePath FROM books WHERE hasContent=1")}
    if references != files.keys():
        raise ValueError("Readable files changed since the verified rehearsal")
    manifest = []
    for index, name in enumerate(sorted(references), 1):
        path = contained(novels, name)
        if path.is_symlink():
            raise ValueError("Unexpected novel symlink")
        observed = fingerprint(path)
        expected = files[name]
        if observed["sha256"] != expected["sha256"] or observed["size"] != expected["size"]:
            raise ValueError("Novel content changed since encoding verification")
        if expected["encoding"] not in ("utf-8", "gb18030", "utf-16le", "utf-16be"):
            raise ValueError("Unsupported encoding evidence")
        manifest.append({"path": name, "encoding": expected["encoding"], "size": observed["size"], "modifiedNs": observed["modifiedNs"]})
        if index % 250 == 0:
            log(f"Verified {index}/{len(references)} original TXT hashes")
    write_json(destination, {"version": 1, "files": manifest})
    destination.chmod(0o644)
    return len(manifest)


def container_args(image, data, name, port):
    command = ["docker", "run", "-d", "--name", name, "--restart", "unless-stopped",
               "--read-only", "--memory", "256m", "--cpus", "1", "--pids-limit", "64",
               "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
               "--tmpfs", "/tmp:size=16m,mode=1777", "--log-opt", "max-size=10m", "--log-opt", "max-file=3",
               "-p", f"127.0.0.1:{port}:8080", "--mount", f"type=bind,src={data},dst=/data/library",
               "--mount", f"type=bind,src={LEGACY / 'novels'},dst=/data/novels,readonly"]
    for key, value in ENV.items():
        command += ["-e", key + "=" + value]
    return command + [image]


def wait_healthy(port):
    for _ in range(60):
        try:
            request(f"http://127.0.0.1:{port}/api/health")
            return
        except (OSError, ValueError):
            time.sleep(1)
    raise RuntimeError("Library healthcheck timed out")


def rollback(state, original):
    if state.get("publicWritesOpened"):
        raise RuntimeError("Legacy rollback refused: the new database may contain public writes")
    for name in (RESTORED, NEW):
        if name in state.get("createdContainers", []) and output("docker", "ps", "-aq", "--filter", f"name=^/{name}$"):
            run("docker", "rm", "-f", name)
    if state.get("oldStopped"):
        run("docker", "update", "--restart=unless-stopped", OLD)
        run("docker", "start", OLD)
        wait_healthy(6870)
    NGINX.write_text(original, encoding="utf-8")
    run("nginx", "-t")
    run("systemctl", "reload", "nginx")
    MARKER.unlink(missing_ok=True)
    state["rolledBack"] = True


def deploy(stage):
    package = verify_bundle(stage)
    if os.geteuid() != 0 or (ROOT / "data").exists() or MARKER.exists():
        raise ValueError("Initial migration requires root, no existing new data and no pending maintenance")
    if shutil.disk_usage("/opt").free < 6 * 1024 ** 3:
        raise ValueError("At least 6 GiB free space is required for migration and independent restore")
    if output("docker", "inspect", "--format", "{{.State.Running}}", OLD) != "true":
        raise ValueError("Expected running legacy library")
    for name in (NEW, RESTORED):
        if output("docker", "ps", "-aq", "--filter", f"name=^/{name}$"):
            raise ValueError("A new library container already exists")
    # Select only non-secret configuration; never dump or read an env file.
    for key, expected in ENV.items():
        if key == "PORTAL_PUBLIC_URL":
            continue  # New profile link; the legacy application did not use it.
        template = '{{range .Config.Env}}{{if eq (index (split . "=") 0) "' + key + '"}}{{.}}{{end}}{{end}}'
        selected = output("docker", "inspect", "--format", template, OLD)
        if selected != key + "=" + expected:
            raise ValueError("Legacy portal contract differs: " + key)
    original = NGINX.read_text(encoding="utf-8")
    if "server_name portal.private.xuexikuaile.fun;" not in original:
        raise ValueError("Expected portal host is absent from nginx")
    gated = maintenance_config(original)
    ident = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + package["revision"][:12]
    release = ROOT / "releases" / ident
    release.parent.mkdir(parents=True, exist_ok=True)
    ROOT.chmod(0o755)  # nginx must be able to stat the maintenance marker.
    shutil.copytree(stage, release, ignore=shutil.ignore_patterns("__pycache__"))
    backup_root = ROOT / "backups" / ident
    backup_root.mkdir(parents=True, mode=0o700)
    backup_root.parent.chmod(0o700)
    reports = ROOT / "reports" / ident
    reports.mkdir(parents=True, mode=0o700)
    (backup_root / "nginx-before.conf").write_text(original, encoding="utf-8")
    state = {"id": ident, "revision": package["revision"], "imageId": package["imageId"],
             "complete": False, "publicWritesOpened": False, "createdContainers": [],
             "startedAt": datetime.now(timezone.utc).isoformat(), "backup": str(backup_root), "reports": str(reports)}
    state["oldImageId"] = output("docker", "inspect", "--format", "{{.Image}}", OLD)
    state["oldRevision"] = output("docker", "inspect", "--format", '{{index .Config.Labels "org.opencontainers.image.revision"}}', OLD)
    write_json(reports / "release.json", state)
    run("docker", "load", "--input", release / "image.tar")
    image = package["imageId"]
    if output("docker", "image", "inspect", "--format", '{{index .Config.Labels "org.opencontainers.image.revision"}}', image) != package["revision"]:
        raise ValueError("Image revision does not match the verified release")
    data = ROOT / "data"
    try:
        log("Entering library maintenance; portal and other sites stay online")
        MARKER.write_text(ident + "\n", encoding="utf-8")
        MARKER.chmod(0o644)
        NGINX.write_text(gated, encoding="utf-8")
        run("nginx", "-t")
        run("systemctl", "reload", "nginx")
        request(ENV["LIBRARY_PUBLIC_URL"] + "/api/v1/me", expected=503)
        state["oldStopped"] = True
        run("docker", "stop", "--time", "30", OLD)
        run("docker", "update", "--restart=no", OLD)
        log("Backing up the final legacy database and covers")
        snapshot = snapshot_legacy(backup_root / "legacy")
        state["legacyCounts"] = snapshot["tables"]
        evidence = json.loads((release / "encoding-evidence.json").read_text(encoding="utf-8"))
        state["novelFiles"] = prepare_manifest(evidence, backup_root / "legacy/library.db", LEGACY / "novels", release / "manifest.json")
        data.mkdir()
        shutil.copytree(backup_root / "legacy/covers", data / "covers")
        (data / "collected").mkdir()
        chown_data(data)
        log("Importing the final snapshot with the release image (1 CPU / 256 MiB)")
        offline = ["docker", "run", "--rm", "--network", "none", "--read-only", "--memory", "256m", "--cpus", "1",
                   "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
                   "--mount", f"type=bind,src={data},dst=/data/library",
                   "--mount", f"type=bind,src={LEGACY / 'novels'},dst=/data/novels,readonly",
                   "--mount", f"type=bind,src={backup_root / 'legacy'},dst=/legacy,readonly",
                   "--mount", f"type=bind,src={release / 'manifest.json'},dst=/manifest.json,readonly", image]
        run(*offline, "migrate", "--db", "/data/library/library.db")
        with (reports / "import.json").open("w", encoding="utf-8") as report:
            run(*offline, "import-legacy", "--source", "/legacy/library.db", "--db", "/data/library/library.db",
                "--novels", "/data/novels", "--manifest", "/manifest.json", "--portal-user", ENV["PORTAL_ALLOWED_USER"],
                "--source-version", state["oldRevision"] + "-" + ident, stdout=report)
        imported = json.loads((reports / "import.json").read_text(encoding="utf-8"))
        if not imported["complete"] or imported["integrityCheck"] != "ok" or imported["schemaVersion"] != 5:
            raise ValueError("Import did not complete its full integrity checks")
        if not imported["tables"] or any(row["sourceRows"] != row["targetRows"] or row["sourceSha256"] != row["targetSha256"] for row in imported["tables"]):
            raise ValueError("Legacy field reconciliation failed")
        state["importSeconds"] = imported["elapsedSeconds"]
        # Drain old nginx workers before starting a new writable backend at the
        # original port. They must not bypass the newly installed marker.
        for _ in range(130):
            if "nginx: worker process is shutting down" not in output("ps", "-eo", "args"):
                break
            time.sleep(1)
        else:
            raise ValueError("Old nginx workers did not drain")
        state["createdContainers"].append(NEW)
        run(*container_args(image, data, NEW, 6870))
        wait_healthy(6870)
        log("Checking final reader ownership, chapter HTTP and all preserved real covers")
        checked = verify(data / "library.db", LEGACY / "novels", "http://127.0.0.1:6870", ENV["PORTAL_ALLOWED_USER"])
        write_json(reports / "candidate.json", checked)
        run("docker", "stop", "--time", "30", NEW)
        log("Creating and independently restoring a new-format backup on the server")
        backed = backup(data, backup_root / "new-format")
        restored = backup_root / "restore-check"
        restore(backup_root / "new-format", restored)
        chown_data(restored)
        state["createdContainers"].append(RESTORED)
        run(*container_args(image, restored, RESTORED, 6871))
        wait_healthy(6871)
        restored_check = verify(restored / "library.db", LEGACY / "novels", "http://127.0.0.1:6871", ENV["PORTAL_ALLOWED_USER"], all_covers=False)
        write_json(reports / "restored.json", restored_check)
        for key in ("personalDigest", "contentDigest"):
            if checked[key] != restored_check[key]:
                raise ValueError("Restored production data differs: " + key)
        run("docker", "rm", "-f", RESTORED)
        state["createdContainers"].remove(RESTORED)
        run("docker", "start", NEW)
        wait_healthy(6870)
        restarted = verify(data / "library.db", LEGACY / "novels", "http://127.0.0.1:6870", ENV["PORTAL_ALLOWED_USER"], all_covers=False)
        write_json(reports / "restarted.json", restarted)
        for key in ("personalDigest", "contentDigest"):
            if checked[key] != restarted[key]:
                raise ValueError("Restarted production data differs: " + key)
        state["backupFiles"] = len(backed["files"])
        # Complete every reversible check before allowing real user writes.
        request(ENV["LIBRARY_PUBLIC_URL"] + "/api/v1/me", expected=503)
        run("nginx", "-t")
        state["publicWritesOpened"] = True
        write_json(reports / "release.json", state)
        MARKER.unlink()
        log("New library public access opened; legacy rollback is now disabled")
        state["publicChecks"] = public_checks(ENV["LIBRARY_PUBLIC_URL"], ENV["PORTAL_AUTH_URL"])
        state["complete"] = True
        state["completedAt"] = datetime.now(timezone.utc).isoformat()
        write_json(ROOT / "current.json", state)
        log("Production deployment, final data reconciliation, restore and public auth checks passed")
    except BaseException as error:
        state["failureType"] = type(error).__name__
        log("Release failed: " + type(error).__name__)
        if not state["publicWritesOpened"]:
            log("Recovering the preserved legacy upstream before public writes")
            rollback(state, original)
        else:
            log("Keeping the new database and service: public writes may already exist")
        raise
    finally:
        write_json(reports / "release.json", state)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: release.py <verified-stage-directory>")
    import fcntl

    def interrupted(*_):
        raise InterruptedError("Deployment interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    with open("/var/lock/personal-library-release.lock", "w") as library_lock, open("/var/lock/personal-portal-release.lock", "w") as portal_lock:
        fcntl.flock(library_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.flock(portal_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        deploy(Path(sys.argv[1]).resolve())
