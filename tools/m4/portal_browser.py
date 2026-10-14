"""Optional real-browser checks for the isolated personal-library portal stack."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import select
import shutil
import socket
import subprocess
import tempfile
import threading


def browser_check(domain, port, client, login, logout, artifacts, check):
    class Proxy(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_CONNECT(self):
            if self.path not in {f"{host}.{domain}:443" for host in ("portal", "auth", "library")}:
                self.send_error(403)
                return
            self.close_connection = True
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=15) as upstream:
                    self.send_response(200)
                    self.end_headers()
                    while True:
                        readable, _, _ = select.select([self.connection, upstream], [], [], 30)
                        if not readable:
                            return
                        for stream in readable:
                            data = stream.recv(65536)
                            if not data:
                                return
                            (upstream if stream is self.connection else self.connection).sendall(data)
            except OSError:
                return

    cli = os.environ.get("AGENT_BROWSER_BIN") or shutil.which("agent-browser")
    if not cli:
        raise RuntimeError("Install agent-browser to use --browser")
    if os.name == "nt" and not str(cli).endswith(".exe"):
        cli = str(Path(cli).parent / "node_modules/agent-browser/bin/agent-browser-win32-x64.exe")
    artifacts.mkdir(parents=True, exist_ok=False)
    proxy = ThreadingHTTPServer(("127.0.0.1", 0), Proxy)
    command = [cli, "--session", "library-portal-ui", "--namespace", f"library-portal-ui-{os.getpid()}", "--json",
               "--proxy", f"http://127.0.0.1:{proxy.server_port}", "--proxy-bypass", "<-loopback>", "--ignore-https-errors"]

    def browser(*arguments):
        # Windows browser daemons can inherit stdout handles; use files instead
        # of pipes, and never log commands containing a synthetic session cookie.
        with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
            try:
                result = subprocess.run(command + list(arguments), stdout=stdout, stderr=stderr, timeout=50)
            except (OSError, subprocess.SubprocessError):
                raise RuntimeError(f"Browser step did not complete: {arguments[0]}; arguments omitted") from None
            if result.returncode:
                raise RuntimeError(f"Browser step failed: {arguments[0]}; arguments and output omitted")
            stdout.seek(0)
            payload = json.loads(stdout.read().decode("utf-8"))
        if not payload.get("success"):
            raise RuntimeError(f"Browser step failed: {arguments[0]}")
        return payload["data"]

    def evaluate(expression):
        return browser("eval", expression)["result"]

    def verify(expression, message):
        check(evaluate(expression) is True, message)

    def set_session():
        for name, value in client.cookies.items():
            browser("cookies", "set", name, value, "--url", f"https://portal.{domain}",
                    "--domain", "." + domain, "--path", "/", "--secure", "--httpOnly", "--sameSite", "Lax")

    def screenshot(name):
        destination = artifacts / (name + ".png")
        result = browser("screenshot", str(destination), "--full")
        if not destination.exists():
            actual = result.get("path") or result.get("screenshotPath")
            if actual:
                shutil.copyfile(actual, destination)
        check(destination.is_file(), "browser screenshot saved: " + name)

    def progress():
        status, raw, _ = client.request("library", "/api/v1/books/1/progress")
        check(status == 200, "browser reading progress is accessible through authenticated gateway")
        return json.loads(raw)["progress"]

    thread = threading.Thread(target=proxy.serve_forever, daemon=True)
    thread.start()
    try:
        browser("open", "about:blank")
        # Session was established by real password + TOTP API requests. The UI
        # login form itself is not part of this library integration check.
        set_session()
        browser("set", "viewport", "1365", "1000")
        browser("open", f"https://portal.{domain}")
        browser("wait", ".app")
        browser("click", ".app")
        browser("wait", "a[href='/profile']")
        verify(f"location.origin === 'https://library.{domain}'", "portal app card opens new library in browser")
        browser("click", "a[href='/profile']")
        browser("wait", ".account-settings")
        screenshot("profile-desktop")
        browser("set", "viewport", "320", "844")
        verify("document.documentElement.scrollWidth <= innerWidth", "profile fits a 320px viewport")
        screenshot("profile-mobile")
        browser("scrollintoview", ".account-settings a[href^='https://portal.']")
        browser("click", ".account-settings a[href^='https://portal.']")
        browser("wait", "#password-fields:not([disabled])")
        verify(f"location.origin === 'https://portal.{domain}' && location.pathname === '/settings' && document.querySelector('#username').value === 'owner'",
               "library account settings opens authenticated portal form")
        screenshot("portal-settings")
        browser("click", ".back-link")
        browser("wait", ".app")
        browser("click", ".app")
        browser("wait", "a[href='/profile']")
        verify(f"location.origin === 'https://library.{domain}'", "portal settings can return through app card to library")
        browser("set", "viewport", "1365", "1000")
        browser("open", f"https://library.{domain}/read/1?c=1")
        browser("wait", ".reader-paragraph")
        browser("wait", "--fn", "document.querySelector('.reader-sync').textContent.includes('进度已')")
        evaluate("document.querySelector('.reader-scroll').scrollTop = 900; true")
        browser("wait", "--text", "进度已同步")
        before = progress()
        check(before is not None and before["percent"] > 0, "browser scroll creates durable reading progress")
        logout()
        paused_top = evaluate("const scroller = document.querySelector('.reader-scroll'); scroller.scrollTop = 2100; scroller.scrollTop")
        browser("wait", "--text", "登录已失效")
        verify("document.body.innerText.includes('阅读位置已留在本机') && [...document.querySelectorAll('.reader-navigation button')].filter(b => ['目录','朗读','下一章 →'].includes(b.textContent)).every(b => b.disabled)",
               "real session loss pauses reader and preserves unsynced position locally")
        check(client.request("library", "/api/v1/me")[0] == 401, "expired browser session also loses API access")
        screenshot("reader-expired")
        print("Re-authenticating browser with real password + a fresh TOTP", flush=True)
        login()
        set_session()
        browser("click", "a[href='/read/1']")
        browser("wait", ".reader-paragraph")
        browser("wait", "--fn", "document.querySelector('.reader-sync').textContent.includes('进度已')")
        resumed_top = evaluate("document.querySelector('.reader-scroll').scrollTop")
        check(abs(resumed_top - paused_top) < 150, "reader restores the unsynced browser position after relogin")
        # Startup replays the pending write before restoring the viewport; its
        # final label may be "restored" even though the write is already saved.
        after = progress()
        check(after["revision"] > before["revision"] and after["percent"] > before["percent"],
              "restored local reading position is synced to the same reader")
        screenshot("reader-restored")
        browser("click", ".reader-back")
        browser("wait", "a[href='/profile']")
        browser("click", "a[href='/profile']")
        browser("wait", ".account-settings")
        browser("click", f"a[href='https://auth.{domain}/logout']")
        browser("wait", "--fn", f"location.origin === 'https://auth.{domain}'")
        browser("wait", "--fn", "!location.pathname.includes('logout')")
        check(client.request("library", "/api/v1/me")[0] == 401, "library exit link invalidates central session")
        errors = browser("errors").get("errors", [])
        check(not errors, "portal and library browser flow has no JavaScript runtime errors")
    except Exception:
        try:
            (artifacts / "failure-snapshot.json").write_text(json.dumps(browser("snapshot", "-i"), ensure_ascii=False, indent=2), encoding="utf-8")
        except Exception:
            pass
        raise
    finally:
        try:
            browser("close")
        finally:
            proxy.shutdown()
            proxy.server_close()
            thread.join(timeout=5)
