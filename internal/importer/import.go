package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EthanHannn/personal-library/internal/legacy"
	"github.com/EthanHannn/personal-library/internal/reader"
	"github.com/EthanHannn/personal-library/internal/storage"
)

type Options struct {
	Source, Target, Novels, PortalUser, SourceVersion string
	Files                                             []reader.File
	BatchSize, MaxBatches                             int
	BatchPause                                        time.Duration
}

type File struct {
	reader.File
	SHA256 string `json:"sha256"`
}

type Comparison struct {
	Entity       string `json:"entity"`
	SourceRows   int64  `json:"sourceRows"`
	TargetRows   int64  `json:"targetRows"`
	SourceSHA256 string `json:"sourceSha256"`
	TargetSHA256 string `json:"targetSha256"`
}

type Report struct {
	Complete       bool                `json:"complete"`
	SchemaVersion  int                 `json:"schemaVersion"`
	SourceVersion  string              `json:"sourceVersion"`
	SourceSHA256   string              `json:"sourceSha256"`
	ManifestSHA256 string              `json:"manifestSha256"`
	Stage          string              `json:"stage"`
	LastRowID      int64               `json:"lastRowid"`
	Tables         []Comparison        `json:"tables,omitempty"`
	Files          []File              `json:"files,omitempty"`
	Legacy         legacy.Verification `json:"legacyVerification"`
	Integrity      string              `json:"integrityCheck,omitempty"`
	Error          string              `json:"error,omitempty"`
	ElapsedSeconds float64             `json:"elapsedSeconds"`
}

func Import(ctx context.Context, o Options) (report Report, result error) {
	started := time.Now()
	report.SchemaVersion, report.SourceVersion = storage.Version, o.SourceVersion
	defer func() {
		report.ElapsedSeconds = time.Since(started).Seconds()
		if result != nil {
			report.Complete = false
			report.Error = result.Error()
		}
	}()
	if o.BatchSize == 0 {
		o.BatchSize = 5000
	}
	if o.BatchSize < 1 || o.BatchSize > 10000 || o.MaxBatches < 0 || o.BatchPause < 0 || o.BatchPause > time.Minute {
		return report, fmt.Errorf("invalid batch limits")
	}
	if o.SourceVersion == "" || o.PortalUser == "" || len(o.Files) == 0 {
		return report, fmt.Errorf("source version, portal user and file manifest are required")
	}
	sourceStat, err := os.Stat(o.Source)
	if err != nil {
		return report, err
	}
	if !sourceStat.Mode().IsRegular() {
		return report, fmt.Errorf("source must be a regular SQLite snapshot")
	}
	targetStat, err := os.Stat(o.Target)
	if err != nil {
		return report, fmt.Errorf("run migrate on a separate target first: %w", err)
	}
	if os.SameFile(sourceStat, targetStat) {
		return report, fmt.Errorf("source and target must be different files")
	}
	if err := noWAL(o.Source); err != nil {
		return report, err
	}
	source, err := storage.Connect(ctx, o.Source, "ro", 1)
	if err != nil {
		return report, err
	}
	defer source.Close()
	snapshot, err := source.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer snapshot.Rollback()
	// Establish a read snapshot before fingerprinting. Source must remain an
	// offline backup; active WAL files are rejected, never silently ignored.
	var dummy int
	if err := snapshot.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master`).Scan(&dummy); err != nil {
		return report, err
	}
	sourceHash, err := hashSnapshot(ctx, o.Source)
	if err != nil {
		return report, err
	}
	report.SourceSHA256 = sourceHash
	if err := snapshot.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id='default-reader'`).Scan(&dummy); err != nil {
		return report, err
	}
	if dummy != 1 {
		return report, fmt.Errorf("legacy default-reader is missing; reader merging is never implicit")
	}
	for _, name := range []string{"users", "books", "chapters", "bookshelf_entries", "reading_progress", "votes", "comments"} {
		if err := snapshot.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT rowid FROM "+name+" WHERE rowid<=0 LIMIT 1)").Scan(&dummy); err != nil {
			return report, err
		}
		if dummy != 0 {
			return report, fmt.Errorf("%s contains unsupported nonpositive rowids", name)
		}
	}
	r, err := reader.Open(o.Novels, 1)
	if err != nil {
		return report, err
	}
	defer r.Close()
	files, manifestHash, err := fingerprintFiles(ctx, r, o.Files)
	if err != nil {
		return report, err
	}
	report.Files, report.ManifestSHA256 = files, manifestHash
	target, err := storage.Open(ctx, o.Target)
	if err != nil {
		return report, err
	}
	defer target.Close()
	if err := initialize(ctx, target, o, report); err != nil {
		return report, err
	}
	batches := 0
	for {
		var stage int
		var last int64
		var status string
		var stored sql.NullString
		if err := target.Read.QueryRowContext(ctx, `SELECT stage,last_rowid,status,report FROM import_runs WHERE id=1`).Scan(&stage, &last, &status, &stored); err != nil {
			return report, err
		}
		if status == "complete" {
			if err := json.Unmarshal([]byte(stored.String), &report); err != nil {
				return report, err
			}
			return report, nil
		}
		report.LastRowID = last
		if stage == len(mappings) {
			break
		}
		if stage < 0 || stage > len(mappings) {
			return report, fmt.Errorf("invalid import stage")
		}
		m := mappings[stage]
		report.Stage = m.name
		if o.MaxBatches > 0 && batches >= o.MaxBatches {
			return report, nil
		}
		count, next, err := copyBatch(ctx, snapshot, target.Write, m, stage, last, o.BatchSize)
		if err != nil {
			return report, fmt.Errorf("%s after rowid %d: %w", m.name, last, err)
		}
		report.LastRowID = next
		if count > 0 {
			batches++
		}
		if o.BatchPause > 0 {
			timer := time.NewTimer(o.BatchPause)
			select {
			case <-ctx.Done():
				timer.Stop()
				return report, ctx.Err()
			case <-timer.C:
			}
		}
	}
	report.Stage = "verification"
	if err := verify(ctx, snapshot, target, r, o, &report); err != nil {
		return report, err
	}
	currentHash, err := hashSnapshot(ctx, o.Source)
	if err != nil {
		return report, err
	}
	if currentHash != sourceHash {
		return report, fmt.Errorf("source snapshot changed during import")
	}
	// Detect source replacement while copying as well as before a resume.
	_, currentManifest, err := fingerprintFiles(ctx, r, o.Files)
	if err != nil {
		return report, err
	}
	if currentManifest != manifestHash {
		return report, reader.ErrChanged
	}
	report.Complete, report.Stage, report.LastRowID = true, "complete", 0
	report.ElapsedSeconds = time.Since(started).Seconds()
	data, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	tx, err := target.Write.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE readers SET portal_user=? WHERE id='default-reader'`, o.PortalUser); err != nil {
		return report, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE import_runs SET status='complete',completed_at=?,report=? WHERE id=1 AND status='running' AND stage=?`, time.Now().UnixMilli(), string(data), len(mappings))
	if err != nil {
		return report, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return report, fmt.Errorf("import checkpoint changed concurrently")
	}
	if err := tx.Commit(); err != nil {
		report.Complete = false
		return report, err
	}
	return report, nil
}

func initialize(ctx context.Context, target *storage.Database, o Options, report Report) error {
	tx, err := target.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var source, manifest, version, user string
	err = tx.QueryRowContext(ctx, `SELECT source_sha256,manifest_sha256,source_version,portal_user FROM import_runs WHERE id=1`).Scan(&source, &manifest, &version, &user)
	if err == nil {
		if source != report.SourceSHA256 || manifest != report.ManifestSHA256 || version != o.SourceVersion || user != o.PortalUser {
			return fmt.Errorf("resume inputs differ from the recorded import; use the original snapshot/config or a fresh target")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, table := range []string{"readers", "books", "files", "chapters", "shelf_entries", "reading_progress", "votes", "comments", "covers", "book_contents", "rating_snapshots"} {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT 1 FROM "+table+" LIMIT 1)").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("target must be empty before its first import")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO import_runs(id,source_sha256,manifest_sha256,source_version,portal_user,status,started_at) VALUES(1,?,?,?,?,'running',?)`, report.SourceSHA256, report.ManifestSHA256, o.SourceVersion, o.PortalUser, time.Now().UnixMilli()); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO files(path,encoding,size,modified_ns,sha256) VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range report.Files {
		if _, err := stmt.ExecContext(ctx, f.Path, f.Encoding, f.Size, f.ModifiedNS, f.SHA256); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func copyBatch(ctx context.Context, source *sql.Tx, target *sql.DB, m mapping, stage int, last int64, limit int) (count int, next int64, result error) {
	rows, err := source.QueryContext(ctx, "SELECT rowid,"+m.fields+" FROM "+m.source+" WHERE rowid>? AND ("+m.predicate+") ORDER BY rowid LIMIT ?", last, limit)
	if err != nil {
		return 0, last, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, last, err
	}
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return 0, last, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, m.insert)
	if err != nil {
		return 0, last, err
	}
	defer stmt.Close()
	next = last
	for rows.Next() {
		values := make([]any, len(columns)-1)
		ptrs := make([]any, len(columns))
		ptrs[0] = &next
		for i := range values {
			ptrs[i+1] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, last, err
		}
		if err := m.normalize(values); err != nil {
			return 0, last, err
		}
		res, err := stmt.ExecContext(ctx, values...)
		if err != nil {
			return 0, last, fmt.Errorf("rowid %d: %w", next, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, last, fmt.Errorf("rowid %d was not mapped exactly once", next)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, last, err
	}
	newStage, newLast := stage, next
	if count < limit {
		newStage++
		newLast = 0
	}
	res, err := tx.ExecContext(ctx, `UPDATE import_runs SET stage=?,last_rowid=? WHERE id=1 AND stage=? AND last_rowid=? AND status='running'`, newStage, newLast, stage, last)
	if err != nil {
		return 0, last, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, last, fmt.Errorf("import checkpoint changed concurrently")
	}
	if err := tx.Commit(); err != nil {
		return 0, last, err
	}
	return count, next, nil
}

func fingerprintFiles(ctx context.Context, r *reader.Reader, input []reader.File) ([]File, string, error) {
	files := make([]File, 0, len(input))
	seen := make(map[string]bool)
	for _, ref := range input {
		ref.Path = strings.ReplaceAll(ref.Path, "\\", "/")
		key := strings.ToLower(ref.Path)
		if seen[key] {
			return nil, "", fmt.Errorf("duplicate or case-colliding manifest path")
		}
		seen[key] = true
		sum, err := r.Fingerprint(ctx, ref)
		if err != nil {
			return nil, "", fmt.Errorf("file %s: %w", ref.Path, err)
		}
		files = append(files, File{ref, sum})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	data, err := json.Marshal(files)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return files, hex.EncodeToString(hash[:]), nil
}

func noWAL(path string) error {
	// Resolve aliases so a symlink cannot hide the real snapshot's WAL.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		stat, err := os.Stat(resolved + suffix)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && stat.Size() > 0 {
			return fmt.Errorf("source has an active %s; create a SQLite-consistent offline backup first", suffix)
		}
	}
	return nil
}

func hashSnapshot(ctx context.Context, path string) (string, error) {
	if err := noWAL(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	after, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return "", fmt.Errorf("source changed while fingerprinting")
	}
	if err := noWAL(path); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
