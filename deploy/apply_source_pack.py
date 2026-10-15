"""Apply one reviewed source to the running library through its local API.

The new source stays disabled until its sample passes. The script records the
result and disables it again if a later check fails. It never touches reader
data, the application database file, or authentication configuration.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import sys
import time
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import Request, urlopen


PRODUCTION_BASE = "http://127.0.0.1:6870"
PRODUCTION_ORIGIN = "https://library.private.xuexikuaile.fun"


def canonical(value):
    if isinstance(value, dict):
        normalized = {key: canonical(item) for key, item in value.items()}
        return {key: item for key, item in normalized.items()
                if item not in (None, "", [], {}, False)}
    if isinstance(value, list):
        return [canonical(item) for item in value]
    return value


def identity(value):
    return "".join(str(value).split()).casefold()


class Client:
    def __init__(self, base, origin):
        parsed = urlparse(base)
        if (parsed.scheme != "http" or parsed.hostname != "127.0.0.1"
                or parsed.path not in ("", "/") or parsed.username or parsed.password
                or parsed.query or parsed.fragment):
            raise ValueError("The management API must be on 127.0.0.1 HTTP")
        self.base = base.rstrip("/")
        self.origin = origin

    def request(self, path, method="GET", body=None):
        payload = json.dumps(body, ensure_ascii=False).encode("utf-8") if body is not None else None
        headers = {"Remote-User": "owner"}
        if payload is not None:
            headers.update({"Origin": self.origin, "Content-Type": "application/json"})
        req = Request(self.base + path, data=payload, headers=headers, method=method)
        try:
            with urlopen(req, timeout=20) as response:
                if response.geturl() != self.base + path:
                    raise ValueError("Unexpected management API redirect")
                return json.load(response)
        except HTTPError as error:
            message = error.read(512).decode("utf-8", errors="replace")
            raise RuntimeError(f"{method} {path}: HTTP {error.code}: {message}") from error

    def wait_job(self, job_id, timeout=300):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            job = self.request("/api/v1/manage/jobs/" + job_id)
            if job["status"] not in ("queued", "running"):
                return job
            time.sleep(1)
        raise TimeoutError(f"Source job {job_id} exceeded {timeout} seconds")

    def set_enabled(self, source, enabled):
        return self.request("/api/v1/manage/sources/" + source["id"], "PUT", {
            "config": source["config"], "enabled": enabled,
            "expectedVersion": source["version"]})


def checked_source(client, source, sample):
    job = client.request("/api/v1/manage/jobs", "POST", {"kind": "source-check", "input": {
        "sourceId": source["id"], "sourceVersion": source["version"],
        "url": sample["url"], "query": sample.get("query", "")}})
    job = client.wait_job(job["id"])
    check = job.get("result", {}).get("sourceCheck", {})
    passed = (job["status"] == "completed" and check.get("passed") is True
              and check.get("sourceId") == source["id"]
              and check.get("version") == source["version"]
              and identity(check.get("title")) == identity(sample["title"])
              and identity(check.get("author")) == identity(sample["author"])
              and check.get("chapters", 0) > 0 and check.get("sampleWords", 0) > 0
              and ("query" not in sample or check.get("searchCount", 0) > 0))
    return passed, {"jobId": job["id"], "status": job["status"], "check": check}


def checked_search(client, source, sample):
    if not sample.get("query"):
        return True, {"skipped": "Source has no search rule"}
    job = client.request("/api/v1/manage/jobs", "POST", {"kind": "search", "input": {
        "sourceIds": [source["id"]], "query": sample["query"]}})
    job = client.wait_job(job["id"])
    candidates = job.get("result", {}).get("searchCandidates", [])
    exact = [item for item in candidates
             if identity(item.get("title")) == identity(sample["title"])
             and identity(item.get("author")) == identity(sample["author"])
             and item.get("url") == sample["url"]]
    passed = job["status"] == "completed" and len(exact) == 1
    return passed, {"jobId": job["id"], "status": job["status"],
                    "candidates": len(candidates), "exactMatches": len(exact)}


def apply(pack_path, expected_sha256, base, origin, report_dir):
    raw = pack_path.read_bytes()
    if len(raw) > 64 * 1024 or hashlib.sha256(raw).hexdigest() != expected_sha256:
        raise ValueError("Source pack is too large or changed during upload")
    pack = json.loads(raw)
    if pack.get("format") != "personal-library/source-pack@1" or len(pack.get("sources", [])) != 1:
        raise ValueError("Expected exactly one reviewed native source")
    entry = pack["sources"][0]
    config, sample = entry["config"], entry["sample"]
    if (config.get("kind") not in ("html", "json") or not config.get("name")
            or not sample.get("url") or not sample.get("title") or not sample.get("author")
            or not entry.get("provenance", {}).get("repository")):
        raise ValueError("Source pack lacks reviewed rules, sample or provenance")
    client = Client(base, origin)
    report = {"at": datetime.now(timezone.utc).isoformat(), "packSha256": expected_sha256,
              "name": config["name"], "sample": sample, "state": "started"}
    source = None
    try:
        before = client.request("/api/v1/manage/sources")["sources"]
        report["before"] = [{"id": item["id"], "name": item["config"]["name"],
                             "enabled": item["enabled"], "version": item["version"]}
                            for item in before]
        if not any(item["id"] == "qidian" and item["enabled"] for item in before):
            raise RuntimeError("Expected enabled built-in Qidian source is missing")
        matches = [item for item in before if item["config"]["name"] == config["name"]]
        if len(matches) > 1:
            raise RuntimeError("Multiple sources already use the reviewed name")
        source = matches[0] if matches else None
        if source and canonical(source["config"]) != canonical(config):
            raise RuntimeError("An existing source has different rules; refusing to replace it")
        if source is None:
            source = client.request("/api/v1/manage/sources", "POST", {
                "config": config, "enabled": False, "expectedVersion": 0})
            report["created"] = source["id"]
        if source["enabled"]:
            source = client.set_enabled(source, False)
        passed, first = checked_source(client, source, sample)
        report["disabledCheck"] = first
        if not passed:
            raise RuntimeError("Source sample failed while disabled")
        source = client.set_enabled(source, True)
        passed, second = checked_source(client, source, sample)
        report["enabledCheck"] = second
        if not passed:
            raise RuntimeError("Source sample failed after enabling")
        search_cases = ([sample, *sample.get("searchChecks", []), sample, sample]
                        if sample.get("query") else [])
        if len(search_cases) > 6:
            raise ValueError("Too many search acceptance cases")
        report["searchIdentity"] = []
        for case in search_cases:
            if any(not case.get(key) for key in ("query", "title", "author", "url")):
                raise ValueError("Search acceptance case is incomplete")
            passed, search = checked_search(client, source, case)
            report["searchIdentity"].append({"query": case["query"], **search})
            if not passed:
                raise RuntimeError("Repeated search did not return exactly one matching title, author and URL")
        final = next(item for item in client.request("/api/v1/manage/sources")["sources"]
                     if item["id"] == source["id"])
        if not final["enabled"] or final["version"] != final["checkVersion"]:
            raise RuntimeError("Final source state is not enabled and checked at the same version")
        report["state"] = "enabled-and-checked"
        report["final"] = {"id": final["id"], "version": final["version"],
                           "enabled": final["enabled"], "checkedAt": final["checkedAt"]}
        client.request("/api/health")
    except Exception as error:
        report["state"] = "failed"
        report["error"] = str(error)
        if source:
            try:
                current = next(item for item in client.request("/api/v1/manage/sources")["sources"]
                               if item["id"] == source["id"])
                if current["enabled"]:
                    report["disabledAfterFailure"] = client.set_enabled(current, False)["version"]
            except Exception as disable_error:
                report["disableError"] = str(disable_error)
        raise
    finally:
        report_dir.mkdir(parents=True, exist_ok=True)
        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        path = report_dir / f"source-pack-{stamp}-{expected_sha256[:12]}.json"
        path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({"state": report["state"], "source": config["name"],
                          "report": str(path), "final": report.get("final"),
                          "error": report.get("error")}, ensure_ascii=False), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("pack", type=Path)
    parser.add_argument("sha256")
    parser.add_argument("--base", default=PRODUCTION_BASE)
    parser.add_argument("--origin", default=PRODUCTION_ORIGIN)
    parser.add_argument("--report-dir", type=Path, default=Path("/opt/personal-library/reports"))
    args = parser.parse_args()
    if len(args.sha256) != 64 or any(c not in "0123456789abcdef" for c in args.sha256):
        parser.error("sha256 must be lowercase hexadecimal")
    apply(args.pack, args.sha256, args.base, args.origin, args.report_dir)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"Source pack failed: {error}", file=sys.stderr)
        raise SystemExit(1) from error
