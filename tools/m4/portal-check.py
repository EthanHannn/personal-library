"""Rehearse the release with personal-portal's real nginx and Authelia config.

Uses only a generated book fixture, random temporary credentials and loopback
HTTPS. Requires a local personal-portal checkout and its Python dependencies.
"""

import argparse
import base64
from datetime import datetime, timezone
import hashlib
import hmac
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import struct
import subprocess
import sys
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
DOMAIN = "private.example.test"


def run(command, *, cwd=None, timeout=180):
    try:
        result = subprocess.run(command, cwd=cwd, capture_output=True, text=True,
                                encoding="utf-8", errors="replace", timeout=timeout)
    except (OSError, subprocess.SubprocessError):
        raise RuntimeError(f"{Path(command[0]).name} did not complete; arguments omitted") from None
    if result.returncode:
        # Commands can contain the temporary TOTP seed. Never echo arguments.
        raise RuntimeError(f"{Path(command[0]).name} failed (exit {result.returncode}); output omitted")
    return result.stdout


def revision(path):
    return {"commit": run(["git", "rev-parse", "HEAD"], cwd=path).strip(),
            "dirty": bool(run(["git", "status", "--porcelain"], cwd=path).strip())}


class Client:
    def __init__(self, port):
        self.port = port
        self.cookies = {}
        # Only connects to the generated loopback stack's self-signed cert.
        self.tls = ssl._create_unverified_context()

    def request(self, host, path="/", *, method="GET", body=None, headers=None):
        port = self.port

        class LocalConnection(http.client.HTTPSConnection):
            def connect(self):
                raw = socket.create_connection(("127.0.0.1", port), timeout=self.timeout)
                self.sock = self._context.wrap_socket(raw, server_hostname=self.host)

        hostname = f"{host}.{DOMAIN}"
        connection = LocalConnection(hostname, timeout=15, context=self.tls)
        values = {"Host": hostname, "Accept": "application/json" if path.startswith("/api/") else "text/html"}
        if self.cookies:
            values["Cookie"] = "; ".join(f"{key}={value}" for key, value in self.cookies.items())
        if body is not None:
            values.update({"Content-Type": "application/json", "Origin": f"https://{hostname}"})
        if headers:
            values.update(headers)
        try:
            connection.request(method, path, body=json.dumps(body) if body is not None else None, headers=values)
            response = connection.getresponse()
            data = response.read().decode("utf-8")
            response_headers = dict((key.lower(), value) for key, value in response.getheaders())
            for key, value in response.getheaders():
                if key.lower() == "set-cookie":
                    parsed = SimpleCookie()
                    parsed.load(value)
                    for name, morsel in parsed.items():
                        if morsel["max-age"] == "0" or not morsel.value:
                            self.cookies.pop(name, None)
                        else:
                            self.cookies[name] = morsel.value
            return response.status, data, response_headers
        finally:
            connection.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--portal-repo", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True, help="new local artifact directory")
    parser.add_argument("--image", default="personal-library:portal-check", help="built release image")
    parser.add_argument("--port", type=int, default=18443)
    parser.add_argument("--browser", action="store_true", help="also exercise the UI with agent-browser")
    args = parser.parse_args()
    portal_repo = args.portal_repo.resolve()
    if not (portal_repo / "tools/portal.py").is_file():
        parser.error("--portal-repo must contain tools/portal.py")
    if not 1024 <= args.port <= 65535:
        parser.error("--port must be an unprivileged TCP port")
    output = args.out.resolve()
    output.mkdir(parents=True, exist_ok=False)
    sys.path.insert(0, str(portal_repo))
    from tools.portal import initialize, render, write_json

    report = {"synthetic": True, "complete": False, "browser": args.browser,
              "startedAt": datetime.now(timezone.utc).isoformat(),
              "library": revision(ROOT), "portal": revision(portal_repo),
              "image": json.loads(run(["docker", "image", "inspect", args.image]))[0]["Id"],
              "checks": []}

    def check(condition, message):
        report["checks"].append({"message": message, "passed": bool(condition)})
        if not condition:
            raise RuntimeError(message)
        print("PASS: " + message, flush=True)

    print("Generating synthetic library and temporary portal configuration", flush=True)
    run([sys.executable, str(ROOT / "tools/m0/fixture.py"), "--output", str(output / "fixture"),
         "--books", "5", "--chapters", "128", "--large-mib", "1"])
    data = output / "library"
    data.mkdir()
    # The generated database is disposable; allow the release UID to initialize
    # it on Linux hosts too, without making any production path writable.
    os.chmod(data, 0o777)
    with tempfile.TemporaryDirectory(prefix="library-portal-check-") as temp:
        stack = Path(temp)
        config = json.loads((portal_repo / "config/portal.example.json").read_text(encoding="utf-8"))
        config.update(domain=DOMAIN, owner="测试读者", username="owner")
        config["config_links"] = {"enabled": False, "default_ttl_minutes": 10}
        password = secrets.token_urlsafe(24)
        seed = base64.b32encode(secrets.token_bytes(20)).decode()
        render(config, stack, local=True)
        initialize(config, stack, password)
        openssl = shutil.which("openssl") or "C:/Program Files/Git/usr/bin/openssl.exe"
        run([openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj",
             "/CN=" + DOMAIN, "-keyout", str(stack / "test.key"), "-out", str(stack / "test.crt")])
        nginx_path = stack / "nginx.conf"
        nginx = nginx_path.read_text(encoding="utf-8")
        for host in ("auth", "portal", "library"):
            nginx = nginx.replace(f"/etc/letsencrypt/live/{host}.{DOMAIN}/fullchain.pem", "/certs/test.crt")
            nginx = nginx.replace(f"/etc/letsencrypt/live/{host}.{DOMAIN}/privkey.pem", "/certs/test.key")
        nginx_path.write_text(nginx, encoding="utf-8")
        (output / "nginx.conf").write_text(nginx, encoding="utf-8")
        auth_path = stack / "authelia/configuration.yml"
        auth = json.loads(auth_path.read_text(encoding="utf-8"))
        auth["ntp"] = {"disable_startup_check": True}
        write_json(auth_path, auth)
        compose_path = stack / "compose.json"
        compose = json.loads(compose_path.read_text(encoding="utf-8"))
        compose["name"] = "library-portal-check-" + uuid.uuid4().hex[:10]
        compose["services"]["nginx"] = {
            "image": "nginx:stable-alpine", "ports": [f"127.0.0.1:{args.port}:443"],
            "volumes": ["./nginx.conf:/etc/nginx/conf.d/default.conf:ro", "./test.crt:/certs/test.crt:ro",
                        "./test.key:/certs/test.key:ro", "./portal:/opt/personal-portal/portal:ro"],
        }
        authelia = compose["services"]["authelia"]
        authelia.pop("ports")
        authelia.update(network_mode="service:nginx", restart="no")
        compose["services"]["library"] = {
            "image": args.image, "network_mode": "service:nginx", "read_only": True,
            "mem_limit": "256m", "cpus": 1, "security_opt": ["no-new-privileges:true"],
            "volumes": [f"{data.as_posix()}:/data/library",
                        f"{(output / 'fixture').as_posix()}:/fixture:ro"],
            "environment": {"PORTAL_ALLOWED_USER": "owner", "LIBRARY_PUBLIC_URL": f"https://library.{DOMAIN}",
                            "PORTAL_AUTH_URL": f"https://auth.{DOMAIN}", "PORTAL_PUBLIC_URL": f"https://portal.{DOMAIN}"},
            "command": ["serve", "--listen", "127.0.0.1:6870", "--db", "/data/library/library.db", "--novels", "/fixture/novels"],
            "healthcheck": {"test": ["CMD", "/library", "healthcheck", "--url", "http://127.0.0.1:6870/api/health"],
                            "interval": "2s", "timeout": "5s", "retries": 20},
        }
        write_json(compose_path, compose)
        compose_command = ["docker", "compose", "-f", str(compose_path)]

        def docker(*arguments):
            return run(compose_command + list(arguments))

        client = Client(args.port)

        def api(path, *, method="GET", body=None, headers=None, status=200):
            actual, raw, response_headers = client.request("library", path, method=method, body=body, headers=headers)
            check(actual == status and "application/json" in response_headers.get("content-type", "")
                  and "no-store" in response_headers.get("cache-control", ""), f"{method} {path}: {status} JSON, no-store")
            return json.loads(raw)

        last_counter = -1

        def login():
            nonlocal last_counter
            check(client.request("auth", "/api/firstfactor", method="POST", body={
                "username": "owner", "password": password, "keepMeLoggedIn": False})[0] == 200, "password accepted")
            check(client.request("library", "/api/v1/me")[0] == 401, "password alone cannot access library API")
            check(client.request("portal", "/apps.json")[0] in (302, 303, 401), "password alone cannot access portal apps")
            while int(time.time()) // 30 <= last_counter:
                time.sleep(0.25)
            last_counter = int(time.time()) // 30
            digest = hmac.new(base64.b32decode(seed), struct.pack(">Q", last_counter), hashlib.sha1).digest()
            offset = digest[-1] & 15
            token = f"{(struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000:06d}"
            check(client.request("auth", "/api/secondfactor/totp", method="POST", body={"token": token})[0] == 200,
                  "TOTP accepted")

        def logout():
            check(client.request("auth", "/api/logout", method="POST", body={})[0] == 200, "Authelia session logged out")

        try:
            docker("config", "--quiet")
            print("Starting isolated nginx and importing the generated library", flush=True)
            docker("up", "-d", "nginx")
            docker("run", "--rm", "--no-deps", "library", "migrate", "--db", "/data/library/library.db")
            imported = docker("run", "--rm", "--no-deps", "library", "import-legacy", "--source", "/fixture/library.db",
                              "--db", "/data/library/library.db", "--novels", "/fixture/novels", "--manifest", "/fixture/manifest.json",
                              "--portal-user", "owner", "--source-version", "synthetic-portal-check")
            (output / "import.json").write_text(imported, encoding="utf-8")
            docker("up", "-d", "--wait", "--wait-timeout", "120")
            docker("exec", "-T", "authelia", "authelia", "storage", "user", "totp", "generate", "owner",
                   "--secret", seed, "--config", "/config/configuration.yml")
            for host, path in (("portal", "/"), ("portal", "/settings"), ("library", "/read/4"), ("library", "/covers/1.jpg")):
                status, _, headers = client.request(host, path, headers={"Remote-User": "owner"})
                check(status == 302 and headers.get("location", "").startswith(f"https://auth.{DOMAIN}/"),
                      f"anonymous {host}{path} redirects to Authelia despite spoofed identity")
            denied = api("/api/v1/me", status=401, headers={"Remote-User": "owner", "X-Forwarded-User": "owner"})
            check(denied.get("error") == "authentication_required", "anonymous API uses portal JSON error contract")
            api("/api/health", status=401)
            check(client.request("library", "/internal/authelia/authz")[0] == 404, "auth subrequest remains internal")
            login()
            status, raw, _ = client.request("portal", "/apps.json")
            apps = json.loads(raw)
            check(status == 200 and len(apps["apps"]) == 1 and apps["apps"][0]["id"] == "library"
                  and apps["apps"][0]["url"] == f"https://library.{DOMAIN}", "portal application opens the same library origin")
            check(client.request("library")[0] == 200, "authenticated library page loads")
            me = api("/api/v1/me", headers={"Remote-User": "forged", "X-Auth-Request-User": "forged"})
            check(me["user"]["id"] == "default-reader" and me["logoutUrl"] == f"https://auth.{DOMAIN}/logout",
                  "nginx replaces forged identity; default reader and central logout remain correct")
            profile = api("/api/v1/profile")
            check(profile["accountUrl"] == f"https://portal.{DOMAIN}/settings", "profile account settings use portal origin")
            status, raw, _ = client.request("portal", "/settings")
            check(status == 200 and 'id="password-form"' in raw, "account settings resolves to real portal form")
            detail = api("/api/v1/books/5")
            check(detail["book"]["title"] == "合成作品 5", "writes target only the synthetic fixture")
            api("/api/v1/books/5/shelf", method="PUT", body={})
            vote = api("/api/v1/books/5/vote", method="PUT", body={"type": "XIANCAO"})
            check(vote["myVote"] == "XIANCAO", "vote saved through real authenticated gateway")
            for origin in ("", f"https://portal.{DOMAIN}"):
                api("/api/v1/books/5/shelf", method="DELETE", body={}, headers={"Origin": origin}, status=403)
            state = api("/api/v1/books/4/reader?chapter=1")
            block = state["content"]
            mutation = {"chapterIdx": 1, "percent": 0.2, "contentVersion": block["contentVersion"],
                        "anchor": {"block": block["blockToken"], "paragraph": 0, "character": 5},
                        "baseRevision": (state["progress"] or {}).get("revision", 0), "deviceId": str(uuid.uuid4()),
                        "clientSeq": 1, "mutationId": str(uuid.uuid4())}
            saved = api("/api/v1/books/4/progress", method="PUT", body=mutation)["progress"]
            check(saved["revision"] == mutation["baseRevision"] + 1, "reading progress saved through real gateway")
            if args.browser:
                from portal_browser import browser_check
                browser_check(DOMAIN, args.port, client, login, logout, output / "browser", check)
            logout()
            api("/api/v1/me", status=401)
            check(client.request("portal", "/apps.json")[0] in (302, 303, 401), "central logout also protects portal")
            print("Re-authenticating in a fresh TOTP time window", flush=True)
            login()
            check(api("/api/v1/me")["user"]["id"] == me["user"]["id"], "relogin preserves default reader")
            check(api("/api/v1/books/5")["inShelf"], "relogin preserves shelf")
            check(api("/api/v1/books/5/vote") == vote, "relogin preserves vote")
            restored = api("/api/v1/books/4/reader")
            check(restored["progress"] == saved and not restored["recoveryRequired"], "relogin restores saved reading anchor")
            check(api("/api/v1/books/4/progress", method="PUT", body=mutation)["progress"] == saved,
                  "replayed progress does not create another revision")
            docker("restart", "library")
            docker("up", "-d", "--wait", "--wait-timeout", "60", "library")
            check(api("/api/v1/books/4/reader")["progress"] == saved, "release restart preserves reading progress")
            docker("stop", "authelia")
            for host, path in (("library", "/api/v1/me"), ("library", "/read/4"), ("portal", "/apps.json")):
                check(client.request(host, path)[0] in (500, 502, 503), f"auth failure denies {host}{path}")
            report["expected"] = {"reader": me["user"]["id"], "progress": saved, "vote": vote}
            report["complete"] = True
        except Exception as error:
            report["error"] = str(error)
            raise
        finally:
            try:
                docker("down", "--remove-orphans")
                report["containersRemoved"] = True
            finally:
                report["finishedAt"] = datetime.now(timezone.utc).isoformat()
                write_json(output / "report.json", report)
    print(f"Complete: {len(report['checks'])} checks; report: {output / 'report.json'}", flush=True)


if __name__ == "__main__":
    main()
