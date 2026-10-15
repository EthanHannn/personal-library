"""Replace the existing Go library container with a checked image, with rollback.

This is an incremental binary update. It never migrates or restores the live
database. The stopped prior container and a consistent SQLite backup are kept.
"""

from datetime import datetime, timezone
import fcntl
import hashlib
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import time
from urllib.request import urlopen


ROOT = Path("/opt/personal-library")
NAME = "personal-library"
OLD_REVISION = "aa33b953d60a73d2cc991a30fa341298c46a48f0"
ENV = {
    "PORTAL_ALLOWED_USER": "owner",
    "LIBRARY_PUBLIC_URL": "https://library.private.xuexikuaile.fun",
    "PORTAL_AUTH_URL": "https://auth.private.xuexikuaile.fun",
    "PORTAL_PUBLIC_URL": "https://portal.private.xuexikuaile.fun",
}


def run(*args, capture=False):
    result = subprocess.run([str(arg) for arg in args], check=True, text=True,
                            capture_output=capture)
    return result.stdout.strip() if capture else None


def inspect(target):
    return json.loads(run("docker", "inspect", target, capture=True))[0]


def file_hash(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def healthy():
    for _ in range(60):
        container = inspect(NAME)
        if container["State"].get("Health", {}).get("Status") == "healthy":
            try:
                with urlopen("http://127.0.0.1:6870/api/health", timeout=3) as response:
                    if response.status == 200:
                        return
            except OSError:
                pass
        time.sleep(1)
    raise RuntimeError("New library did not become healthy")


def verify_current():
    container = inspect(NAME)
    config, host = container["Config"], container["HostConfig"]
    if (not container["State"]["Running"] or
            container["State"].get("Health", {}).get("Status") != "healthy" or
            config["Labels"].get("org.opencontainers.image.revision") != OLD_REVISION):
        raise ValueError("Expected healthy production Go library at the recorded revision")
    mounts = {item["Destination"]: (item["Source"], item["RW"])
              for item in container["Mounts"]}
    if mounts != {"/data/library": (str(ROOT / "data"), True),
                  "/data/novels": ("/opt/zhixuan-library/novels", False)}:
        raise ValueError("Production data mounts differ from the reviewed layout")
    if host["PortBindings"].get("8080/tcp") != [{"HostIp": "127.0.0.1", "HostPort": "6870"}]:
        raise ValueError("Production loopback port differs from the reviewed layout")
    values = set(config["Env"])
    if any(key + "=" + value not in values for key, value in ENV.items()):
        raise ValueError("Production gateway environment differs from the reviewed layout")
    return container


def backup_db(destination):
    destination.parent.mkdir(parents=True, mode=0o700)
    with sqlite3.connect((ROOT / "data/library.db").as_uri() + "?mode=ro", uri=True) as source:
        with sqlite3.connect(destination) as copy:
            source.backup(copy, pages=512, sleep=0.05)
            if copy.execute("PRAGMA quick_check").fetchone()[0] != "ok":
                raise ValueError("SQLite backup quick_check failed")
    destination.chmod(0o600)


def update(archive, expected_hash, revision):
    if (not re.fullmatch(r"[0-9a-f]{64}", expected_hash) or
            not re.fullmatch(r"[0-9a-f]{40}", revision) or
            archive.stat().st_size > 300 * 1024 * 1024 or
            file_hash(archive) != expected_hash):
        raise ValueError("Image archive identity or size is invalid")
    verify_current()
    image = "personal-library:release-" + revision[:12]
    run("docker", "load", "--input", archive)
    if inspect(image)["Config"]["Labels"].get("org.opencontainers.image.revision") != revision:
        raise ValueError("Loaded image revision does not match the reviewed commit")
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    previous = NAME + "-previous-" + stamp
    report_path = ROOT / "reports" / ("image-update-" + stamp + "-" + revision[:12] + ".json")
    report = {"at": stamp, "oldRevision": OLD_REVISION, "revision": revision,
              "archiveSha256": expected_hash, "previousContainer": previous,
              "backup": str(ROOT / "backups" / ("image-update-" + stamp) / "library.db"),
              "state": "started"}
    stopped = renamed = False
    try:
        run("docker", "stop", "--time", "30", NAME)
        stopped = True
        backup_db(Path(report["backup"]))
        run("docker", "rename", NAME, previous)
        renamed = True
        run("docker", "update", "--restart=no", previous)
        args = ["docker", "run", "-d", "--name", NAME, "--restart", "unless-stopped",
                "--read-only", "--memory", "256m", "--cpus", "1", "--pids-limit", "64",
                "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
                "--tmpfs", "/tmp:size=16m,mode=1777", "--log-opt", "max-size=10m",
                "--log-opt", "max-file=3", "-p", "127.0.0.1:6870:8080",
                "--mount", f"type=bind,src={ROOT / 'data'},dst=/data/library",
                "--mount", "type=bind,src=/opt/zhixuan-library/novels,dst=/data/novels,readonly"]
        for key, value in ENV.items():
            args.extend(["-e", key + "=" + value])
        run(*args, image)
        healthy()
        if inspect(NAME)["Config"]["Labels"].get("org.opencontainers.image.revision") != revision:
            raise ValueError("Running container revision does not match the reviewed commit")
        report["state"] = "healthy"
    except BaseException as error:
        report["state"] = "failed"
        report["error"] = str(error)
        try:
            if renamed and run("docker", "ps", "-aq", "--filter", f"name=^/{NAME}$", capture=True):
                run("docker", "rm", "-f", NAME)
            if renamed:
                run("docker", "rename", previous, NAME)
            if stopped:
                run("docker", "update", "--restart=unless-stopped", NAME)
                run("docker", "start", NAME)
                healthy()
            report["rollback"] = "prior container healthy"
        except BaseException as rollback_error:
            report["rollback"] = "failed: " + str(rollback_error)
        raise
    finally:
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"state": report["state"], "report": str(report_path),
                          "rollback": report.get("rollback")}, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("Usage: update_image.py <image.tar> <sha256> <revision>")
    with open("/var/lock/personal-library-release.lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        update(Path(sys.argv[1]), sys.argv[2], sys.argv[3])
