"""Read-only checks for a migrated library on a loopback port (no credentials)."""

import argparse
from contextlib import closing
import hashlib
import json
from pathlib import Path
import sqlite3
import time
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def request(url, user=None, expected=200):
    headers = {"Remote-User": user} if user else {}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        response = opener.open(urllib.request.Request(url, headers=headers), timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        if response.code != expected:
            raise ValueError(f"Unexpected HTTP {response.code} for {urllib.parse.urlsplit(url).path}")
        return response.headers, response.read()


def public_checks(library, auth):
    for path in ("/api/health", "/api/v1/me", "/api/v1/books", "/api/v1/books/1/chapters/1/content"):
        for user in (None, "owner"):
            headers, data = request(library + path, user, expected=401)
            if "application/json" not in headers.get("Content-Type", ""):
                raise ValueError("Public API did not return an authentication JSON response")
            json.loads(data)
    headers, _ = request(library + "/", expected=302)
    if urllib.parse.urlsplit(headers.get("Location", "")).netloc != urllib.parse.urlsplit(auth).netloc:
        raise ValueError("Library login redirect does not point to the portal identity service")
    return {"anonymousAndForgedIdentity": 8, "loginRedirect": True}


def verify(database, novels, base, user, *, all_covers=True):
    if urllib.parse.urlsplit(base).hostname != "127.0.0.1":
        raise ValueError("Private checks only accept loopback endpoints")
    report = {"complete": False, "requests": 0, "books": 0, "chapters": 0, "blocks": 0, "covers": 0}
    started = time.monotonic()
    digest = hashlib.sha256()

    def get(path, binary=False):
        headers, data = request(base + path, user)
        report["requests"] += 1
        return data if binary else json.loads(data)

    def page(route):
        entries, cursor = [], None
        while True:
            result = get(route + "?limit=48" + ("&cursor=" + urllib.parse.quote(cursor) if cursor else ""))
            entries.extend(result["entries"])
            cursor = result.get("nextCursor")
            if not cursor:
                return entries
            if len(entries) > 100000:
                raise ValueError("Collection cursor did not terminate")

    with closing(sqlite3.connect(database.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        db.row_factory = sqlite3.Row
        me = get("/api/v1/me")
        if me["user"]["id"] != "default-reader" or not me["logoutUrl"].endswith("/logout"):
            raise ValueError("Portal reader mapping or logout URL changed")
        expected = {}
        for api, table in (("shelf", "shelf_entries"), ("reading", "reading_progress"), ("votes", "votes")):
            rows = list(db.execute(f"SELECT * FROM {table} WHERE reader_id='default-reader' ORDER BY book_id"))
            entries = page("/api/v1/" + api)
            if sorted(entry["book"]["id"] for entry in entries) != [row["book_id"] for row in rows]:
                raise ValueError("Personal record ownership changed: " + api)
            expected[api] = len(rows)
        profile = get("/api/v1/profile")
        if profile["counts"] != expected or not profile["accountUrl"].endswith("/settings"):
            raise ValueError("Profile counts or account settings URL changed")
        personal = []
        for row in db.execute("SELECT book_id FROM shelf_entries UNION SELECT book_id FROM reading_progress UNION SELECT book_id FROM votes ORDER BY book_id"):
            ident = row[0]
            detail = get(f"/api/v1/books/{ident}")
            shelf = db.execute("SELECT count(*) FROM shelf_entries WHERE reader_id='default-reader' AND book_id=?", (ident,)).fetchone()[0]
            progress = db.execute("SELECT chapter_idx,percent FROM reading_progress WHERE reader_id='default-reader' AND book_id=?", (ident,)).fetchone()
            vote = db.execute("SELECT type FROM votes WHERE reader_id='default-reader' AND book_id=?", (ident,)).fetchone()
            if detail["inShelf"] != bool(shelf) or detail["vote"]["myVote"] != (vote[0] if vote else None):
                raise ValueError("Shelf/vote differs from the migrated reader")
            observed = detail["progress"]
            if (progress is None) != (observed is None):
                raise ValueError("Progress was implicitly merged between readers")
            if progress and (observed["chapterIdx"], observed["percent"]) != tuple(progress):
                raise ValueError("Progress differs from the migrated reader")
            personal.append({"bookId": ident, "inShelf": detail["inShelf"], "progress": observed, "vote": detail["vote"]})
        report["personalDigest"] = hashlib.sha256(json.dumps(personal, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
        report["defaultReaderCounts"] = expected
        report["readers"] = db.execute("SELECT count(*) FROM readers").fetchone()[0]
        # The import already checks every chapter and hashes every TXT. Exercise
        # production HTTP for each encoding, both ends of the catalogue and all
        # readable books with personal state, without writing test user data.
        selection = {row[0] for row in db.execute("SELECT id FROM books WHERE has_content=1 ORDER BY id LIMIT 8")}
        selection.update(row[0] for row in db.execute("SELECT id FROM books WHERE has_content=1 ORDER BY id DESC LIMIT 8"))
        selection.update(row[0] for row in db.execute("SELECT min(b.id) FROM books b JOIN book_contents c ON c.book_id=b.id AND c.is_current=1 JOIN files f ON f.id=c.file_id WHERE b.has_content=1 GROUP BY f.encoding"))
        selection.update(item["bookId"] for item in personal if db.execute("SELECT has_content FROM books WHERE id=?", (item["bookId"],)).fetchone()[0])
        for ident in sorted(selection):
            book = db.execute("SELECT chapter_count FROM books WHERE id=?", (ident,)).fetchone()
            for idx in sorted({1, max(1, (book[0] + 1) // 2), book[0]}):
                chapter = db.execute("SELECT c.title,c.start_offset,c.end_offset FROM chapters c JOIN book_contents bc ON bc.book_id=c.book_id AND bc.version=c.content_version AND bc.is_current=1 WHERE c.book_id=? AND c.idx=?", (ident, idx)).fetchone()
                offset, cursor, blocks = chapter[1], None, 0
                while True:
                    path = f"/api/v1/books/{ident}/chapters/{idx}/content"
                    block = get(path + ("?cursor=" + urllib.parse.quote(cursor) if cursor else ""))
                    if (block["chapter"]["title"], block["chapterStart"], block["chapterEnd"], block["blockStart"]) != (chapter[0], chapter[1], chapter[2], offset):
                        raise ValueError("Production chapter metadata or cursor differs")
                    if not offset < block["blockEnd"] <= chapter[2] or not block["text"]:
                        raise ValueError("Invalid chapter block")
                    digest.update(json.dumps([ident, idx, offset, block["blockEnd"], block["text"]], ensure_ascii=False).encode())
                    offset, cursor = block["blockEnd"], block.get("nextCursor")
                    report["blocks"] += 1
                    blocks += 1
                    if blocks > 10000:
                        raise ValueError("Content cursor did not terminate")
                    if not cursor:
                        break
                if offset != chapter[2]:
                    raise ValueError("Chapter ended before its original byte boundary")
                report["chapters"] += 1
            report["books"] += 1
        covers = db.execute("SELECT b.id,c.legacy_path FROM books b JOIN covers c ON c.book_id=b.id WHERE b.status='APPROVED' AND c.legacy_path IS NOT NULL ORDER BY b.id")
        for ident, name in covers:
            if Path(name).suffix.lower() not in (".jpg", ".jpeg", ".png", ".webp", ".gif"):
                continue
            if not name.startswith("/covers/"):
                raise ValueError("Unexpected cover location")
            path = database.parent / name.lstrip("/")
            if not path.resolve().is_relative_to((database.parent / "covers").resolve()):
                raise ValueError("Cover path escapes its root")
            data = get(f"/api/v1/books/{ident}/cover", binary=True)
            if hashlib.sha256(data).digest() != hashlib.sha256(path.read_bytes()).digest():
                raise ValueError("Production cover differs from its preserved asset")
            report["covers"] += 1
            if not all_covers:
                break
        for path in ("/", "/library", "/profile", "/manage", f"/read/{min(selection)}"):
            if b'<div id="app"' not in get(path, binary=True):
                raise ValueError("Release frontend or deep link is missing")
        for path in ("/api/v1/home", "/api/v1/categories", "/api/v1/books?limit=24", "/api/v1/manage/sources"):
            get(path)
        request(base + "/api/v1/me", expected=401)
        request(base + "/api/v1/me", "not-the-owner", expected=403)
        report["contentDigest"] = digest.hexdigest()
        report["complete"] = True
        report["elapsedSeconds"] = round(time.monotonic() - started, 2)
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", type=Path, required=True)
    parser.add_argument("--novels", type=Path, required=True)
    parser.add_argument("--base", default="http://127.0.0.1:6870")
    parser.add_argument("--user", default="owner")
    args = parser.parse_args()
    print(json.dumps(verify(args.db, args.novels, args.base, args.user)))
