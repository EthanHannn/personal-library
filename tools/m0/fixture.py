"""Create an isolated, deterministic legacy-schema fixture with no private data.

Only writes a new directory. Uses bounded insert batches and streaming file I/O.
The repeated file references are intentional: query scale and text size can be
measured independently. This is not a substitute for real migration validation.
"""

import argparse
import hashlib
import json
import platform
import sqlite3
import time
from pathlib import Path


SCHEMA = """
CREATE TABLE users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, nickname TEXT,
 role TEXT NOT NULL DEFAULT 'USER', createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updatedAt DATETIME NOT NULL
);
CREATE TABLE books (
 id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, author TEXT NOT NULL,
 tag1 TEXT NOT NULL, tag2 TEXT NOT NULL, size TEXT NOT NULL, intro TEXT NOT NULL,
 popularity INTEGER NOT NULL DEFAULT 0, totalScore INTEGER NOT NULL DEFAULT 0,
 score REAL NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'APPROVED',
 xiancaoCount INTEGER NOT NULL DEFAULT 0, liangcaoCount INTEGER NOT NULL DEFAULT 0,
 gancaoCount INTEGER NOT NULL DEFAULT 0, kucaoCount INTEGER NOT NULL DEFAULT 0,
 ducaoCount INTEGER NOT NULL DEFAULT 0, submittedById TEXT,
 postId INTEGER, filePath TEXT, chapterCount INTEGER NOT NULL DEFAULT 0,
 wordCount INTEGER NOT NULL DEFAULT 0, hasContent BOOLEAN NOT NULL DEFAULT 0,
 coverPath TEXT, coverSource TEXT, coverSourceUrl TEXT, coverFetchedAt DATETIME,
 createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updatedAt DATETIME NOT NULL,
 FOREIGN KEY (submittedById) REFERENCES users(id)
);
CREATE TABLE chapters (
 id INTEGER PRIMARY KEY AUTOINCREMENT, bookId INTEGER NOT NULL, idx INTEGER NOT NULL,
 title TEXT NOT NULL, startOffset INTEGER NOT NULL, endOffset INTEGER NOT NULL,
 FOREIGN KEY (bookId) REFERENCES books(id) ON DELETE CASCADE
);
CREATE INDEX chapters_bookId_idx_idx ON chapters(bookId, idx);
CREATE INDEX books_score_idx ON books(score);
CREATE INDEX books_createdAt_idx ON books(createdAt);
CREATE INDEX books_wordCount_idx ON books(wordCount);
CREATE INDEX books_xiancaoCount_idx ON books(xiancaoCount);
CREATE TABLE reading_progress (
 id TEXT PRIMARY KEY, userId TEXT NOT NULL, bookId INTEGER NOT NULL,
 chapterIdx INTEGER NOT NULL DEFAULT 1, percent REAL NOT NULL DEFAULT 0, updatedAt DATETIME NOT NULL,
 UNIQUE(userId,bookId)
);
CREATE TABLE bookshelf_entries (
 id TEXT PRIMARY KEY, userId TEXT NOT NULL, bookId INTEGER NOT NULL,
 addedAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updatedAt DATETIME NOT NULL, UNIQUE(userId,bookId)
);
CREATE TABLE votes (
 id TEXT PRIMARY KEY, type TEXT NOT NULL, userId TEXT NOT NULL, bookId INTEGER NOT NULL,
 createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(userId,bookId)
);
CREATE TABLE comments (
 id TEXT PRIMARY KEY, content TEXT NOT NULL, userId TEXT NOT NULL, bookId INTEGER NOT NULL,
 parentId TEXT, likeCount INTEGER NOT NULL DEFAULT 0, dislikeCount INTEGER NOT NULL DEFAULT 0,
 createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updatedAt DATETIME NOT NULL
);
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--chapters", type=int, default=5_844_097)
    parser.add_argument("--books", type=int, default=6761)
    parser.add_argument("--large-mib", type=int, default=256)
    args = parser.parse_args()
    chunk_bytes = 16 * 1024
    large_count = args.large_mib * 1024 * 1024 // chunk_bytes
    if args.books < 5 or args.large_mib < 1 or args.chapters < large_count + args.books + 1:
        parser.error("need >=5 books and enough chapters for the requested large file")
    out = args.output.resolve()
    if out.exists() and any(out.iterdir()):
        parser.error("output must be a new or empty directory; existing data is never replaced")
    novel_root = out / "novels"
    novel_root.mkdir(parents=True, exist_ok=True)
    started = time.monotonic()
    manifest = {"version": 1, "files": []}
    hashes = {}

    def register(name, encoding):
        path = novel_root / name
        stat = path.stat()
        manifest["files"].append({"path": name, "encoding": encoding,
                                  "size": stat.st_size, "modifiedNs": str(stat.st_mtime_ns)})
        with path.open("rb") as source:
            hashes[name] = hashlib.file_digest(source, "sha256").hexdigest()

    sentence = "这是独立生成的性能测试正文，包含中文、换行和四字节字符𠀀😀。\r\n".encode()
    with (novel_root / "large.txt").open("wb") as target:
        for idx in range(1, large_count + 1):
            title = f"第{idx}章 合成样本\r\n".encode()
            room = chunk_bytes - len(title) - 1
            repeats, padding = divmod(room, len(sentence))
            target.write(title + sentence * repeats + b" " * padding + b"\n")
    register("large.txt", "utf-8")
    small = "第一章 边界\r\n\r\n中文𠀀😀。\r\n第二章 末尾\n末行没有换行"
    special = {}
    for name, encoding, bom in [("bom.txt", "utf-8", b"\xef\xbb\xbf"), ("gb.txt", "gb18030", b"")]:
        data = bom + small.encode(encoding)
        split = len(bom) + len(small[:small.index("第二章")].encode(encoding))
        (novel_root / name).write_bytes(data)
        special[name] = [(0, split, "第一章 边界"), (split, len(data), "第二章 末尾")]
        register(name, encoding)
    (novel_root / "long-line.txt").write_bytes(("没有分章的超长行𠀀😀。" * 70000).encode())
    register("long-line.txt", "utf-8")
    long_size = (novel_root / "long-line.txt").stat().st_size
    remaining = args.chapters - large_count - 5
    per_book, extra = divmod(remaining, args.books - 4)
    if per_book + bool(extra) > large_count:
        parser.error("large file needs more chapters for this index/book combination")
    counts = [large_count, 2, 2, 1] + [per_book + (i < extra) for i in range(args.books - 4)]
    stamp = 1_791_849_600_000  # Fixed synthetic timestamp; no live personal metadata.
    with sqlite3.connect(out / "library.db") as db:
        db.executescript("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA cache_size=-8192; PRAGMA temp_store=FILE;" + SCHEMA)
        db.execute("INSERT INTO users(id,username,nickname,role,updatedAt) VALUES('default-reader','default-reader','测试读者','ADMIN',?)", (stamp,))
        batch = []
        inserted = 0
        for book_id, count in enumerate(counts, 1):
            name = {2: "bom.txt", 3: "gb.txt", 4: "long-line.txt"}.get(book_id, "large.txt")
            db.execute("""INSERT INTO books(id,title,author,tag1,tag2,size,intro,score,status,filePath,chapterCount,wordCount,hasContent,updatedAt)
                       VALUES(?,?,?,'测试','合成','测试样本','无真实正文或个人数据',9,'APPROVED',?,?,?,1,?)""",
                       (book_id, f"合成作品 {book_id}", f"测试作者 {book_id % 53}", name, count, count * 4000, stamp))
            for idx in range(1, count + 1):
                if name in special:
                    start, end, title = special[name][idx - 1]
                elif name == "long-line.txt":
                    start, end, title = 0, long_size, "全文"
                else:
                    start, end, title = (idx - 1) * chunk_bytes, idx * chunk_bytes, f"第{idx}章 合成样本"
                batch.append((book_id, idx, title, start, end))
                if len(batch) >= 5000:
                    db.executemany("INSERT INTO chapters(bookId,idx,title,startOffset,endOffset) VALUES(?,?,?,?,?)", batch)
                    inserted += len(batch)
                    batch.clear()
                    db.commit()
                    if inserted % 500000 == 0:
                        print(f"indexed {inserted:,} chapters", flush=True)
        if batch:
            db.executemany("INSERT INTO chapters(bookId,idx,title,startOffset,endOffset) VALUES(?,?,?,?,?)", batch)
        db.commit()
        count = db.execute("SELECT count(*) FROM chapters").fetchone()[0]
        assert count == args.chapters
        check = db.execute("PRAGMA quick_check").fetchone()[0]
        assert check == "ok", check
        assert not db.execute("PRAGMA foreign_key_check").fetchall()
        db.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        db.execute("PRAGMA journal_mode=DELETE")
        plan = db.execute("EXPLAIN QUERY PLAN SELECT title,startOffset,endOffset FROM chapters WHERE bookId=? AND idx=?", (1, 8000)).fetchall()
    (out / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    with (out / "library.db").open("rb") as source:
        database_hash = hashlib.file_digest(source, "sha256").hexdigest()
    summary = {"synthetic": True, "books": args.books, "chapters": args.chapters,
               "largeFileBytes": args.large_mib * 1024 * 1024, "largeFileChapters": large_count,
               "databaseBytes": (out / "library.db").stat().st_size,
               "databaseHash": database_hash,
               "fileHashes": hashes, "queryPlan": plan, "sqlite": sqlite3.sqlite_version,
               "python": platform.python_version(), "generationSeconds": round(time.monotonic() - started, 3)}
    (out / "fixture-summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
