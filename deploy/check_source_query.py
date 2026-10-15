"""Run one bounded production search through a reviewed, enabled book source."""

import hashlib
import json
import argparse
from pathlib import Path
import sys

from apply_source_pack import Client, PRODUCTION_BASE, PRODUCTION_ORIGIN, canonical


def check(pack_path, expected_hash, query, base=PRODUCTION_BASE, origin=PRODUCTION_ORIGIN):
    raw = Path(pack_path).read_bytes()
    if (len(raw) > 64 * 1024 or hashlib.sha256(raw).hexdigest() != expected_hash or
            not query or len(query) > 80 or any(ord(char) < 32 for char in query)):
        raise ValueError("Reviewed pack or query is invalid")
    pack = json.loads(raw)
    if pack.get("format") != "personal-library/source-pack@1" or len(pack.get("sources", [])) != 1:
        raise ValueError("Expected one reviewed native source")
    config = pack["sources"][0]["config"]
    if not config.get("search"):
        raise ValueError("Reviewed source has no search rule")
    client = Client(base, origin)
    matches = [source for source in client.request("/api/v1/manage/sources")["sources"]
               if source["config"]["name"] == config["name"]]
    if (len(matches) != 1 or not matches[0]["enabled"] or
            matches[0]["version"] != matches[0]["checkVersion"] or
            canonical(matches[0]["config"]) != canonical(config)):
        raise RuntimeError("Production source does not match the reviewed enabled rules")
    source = matches[0]
    job = client.request("/api/v1/manage/jobs", "POST", {"kind": "search", "input": {
        "sourceIds": [source["id"]], "query": query}})
    job = client.wait_job(job["id"], timeout=90)
    reports = job.get("result", {}).get("searchSources", [])
    errors = [item.get("error") for item in reports if item.get("error")]
    if job["status"] != "completed" or errors:
        raise RuntimeError("Production search failed: " + ", ".join(errors or [job["status"]]))
    candidates = job.get("result", {}).get("searchCandidates", [])
    print(json.dumps({"source": config["name"], "query": query, "jobId": job["id"],
                      "count": len(candidates), "candidates": [
                          {"title": item.get("title"), "author": item.get("author"),
                           "url": item.get("url")} for item in candidates[:20]]},
                     ensure_ascii=False), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("pack")
    parser.add_argument("sha256")
    parser.add_argument("--base", default=PRODUCTION_BASE)
    parser.add_argument("--origin", default=PRODUCTION_ORIGIN)
    args = parser.parse_args()
    check(args.pack, args.sha256, sys.stdin.read().strip(), args.base, args.origin)
