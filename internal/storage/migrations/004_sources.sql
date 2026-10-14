CREATE TABLE source_configs (
 id TEXT PRIMARY KEY NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 config TEXT NOT NULL,
 checked_at INTEGER NOT NULL DEFAULT 0,
 check_version INTEGER NOT NULL DEFAULT 0,
 check_result TEXT NOT NULL DEFAULT '{}'
) STRICT;
INSERT INTO source_configs(id,version,enabled,config) VALUES
 ('qidian',1,1,'{"name":"起点中文网","group":"男频","kind":"qidian","baseUrl":"https://www.qidian.com/","allowedHosts":["www.qidian.com","m.qidian.com","book.qidian.com"],"charset":"utf-8","intervalMs":5000}');

CREATE TABLE library_jobs_v4 (
 id TEXT PRIMARY KEY NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('import','collect','cover','scan','search','source-check')),
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
INSERT INTO library_jobs_v4 SELECT * FROM library_jobs;
CREATE TABLE job_chapters_v4 (
 job_id TEXT NOT NULL REFERENCES library_jobs_v4(id),
 idx INTEGER NOT NULL,
 title TEXT NOT NULL,
 start_offset INTEGER NOT NULL,
 end_offset INTEGER NOT NULL,
 PRIMARY KEY(job_id,idx)
) STRICT;
INSERT INTO job_chapters_v4 SELECT * FROM job_chapters;
DROP TABLE job_chapters;
DROP TABLE library_jobs;
ALTER TABLE library_jobs_v4 RENAME TO library_jobs;
ALTER TABLE job_chapters_v4 RENAME TO job_chapters;
CREATE UNIQUE INDEX jobs_active ON library_jobs(kind,dedupe) WHERE status IN ('queued','running','review');
CREATE INDEX jobs_recent ON library_jobs(created_at DESC,id);

CREATE TABLE book_sources_v4 (
 book_id INTEGER NOT NULL REFERENCES books(id),
 source TEXT NOT NULL,
 source_id TEXT NOT NULL,
 source_url TEXT NOT NULL,
 complete INTEGER NOT NULL CHECK(complete IN (0,1)),
 rule_version INTEGER NOT NULL DEFAULT 0,
 collected_at INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(book_id,source,source_id),
 UNIQUE(source,source_id)
) STRICT;
INSERT INTO book_sources_v4(book_id,source,source_id,source_url,complete) SELECT * FROM book_sources;
DROP TABLE book_sources;
ALTER TABLE book_sources_v4 RENAME TO book_sources;
