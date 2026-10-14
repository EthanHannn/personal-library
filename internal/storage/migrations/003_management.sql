CREATE TABLE library_jobs (
 id TEXT PRIMARY KEY NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('import','collect','cover','scan')),
 dedupe TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','running','review','completed','failed','cancelled')),
 payload TEXT NOT NULL,
 result TEXT NOT NULL DEFAULT '{}',
 message TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0,
 next_at INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX jobs_active ON library_jobs(kind,dedupe) WHERE status IN ('queued','running','review');
CREATE INDEX jobs_recent ON library_jobs(created_at DESC,id);
CREATE TABLE job_chapters (
 job_id TEXT NOT NULL REFERENCES library_jobs(id),
 idx INTEGER NOT NULL,
 title TEXT NOT NULL,
 start_offset INTEGER NOT NULL,
 end_offset INTEGER NOT NULL,
 PRIMARY KEY(job_id,idx)
) STRICT;
CREATE TABLE source_cooldowns (
 source TEXT PRIMARY KEY NOT NULL,
 next_at INTEGER NOT NULL
) STRICT;
CREATE TABLE cover_assets (
 book_id INTEGER PRIMARY KEY REFERENCES books(id),
 path TEXT NOT NULL,
 source_url TEXT NOT NULL,
 updated_at INTEGER NOT NULL
) STRICT;
CREATE TABLE book_sources (
 book_id INTEGER PRIMARY KEY REFERENCES books(id),
 source TEXT NOT NULL,
 source_id TEXT NOT NULL,
 source_url TEXT NOT NULL,
 complete INTEGER NOT NULL CHECK(complete IN (0,1)),
 UNIQUE(source,source_id)
) STRICT;
