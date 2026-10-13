"""Make an independent catalog rehearsal DB; never mutate historical fixtures."""
import argparse
import json
import sqlite3
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('--source', required=True)
parser.add_argument('--output', required=True)
args = parser.parse_args()
source = Path(args.source).resolve()
output = Path(args.output).resolve()
if output.exists():
    raise SystemExit('Output must not exist; use a new rehearsal directory')
with sqlite3.connect(source.as_uri() + '?mode=ro', uri=True) as old:
    if old.execute('SELECT title FROM books WHERE id=5').fetchone() != ('合成作品 5',):
        raise SystemExit('Synthetic M0-derived business fixture required')
    output.mkdir(parents=True)
    with sqlite3.connect(output / 'library.db') as new:
        old.backup(new)
        # Vary only metadata, retaining all IDs, chapter rows, files and private
        # relationships. The first five books stay compatible with M1-M3 smoke.
        tags = [('奇幻', '史诗'), ('仙侠', '修真'), ('科幻', '太空'), ('历史', '架空'), ('都市', '生活'), ('武侠', '江湖')]
        scores = [0, 6.5, 7.5, 8, 9, 10]
        ids = [row[0] for row in new.execute('SELECT id FROM books WHERE id>5')]
        for ident in ids:
            main, sub = tags[ident % len(tags)]
            new.execute('UPDATE books SET author=?,tag1=?,tag2=?,word_count=?,created_at=?,has_content=? WHERE id=?',
                        (f'合成作者 {ident % 47}', main, sub, (ident % 60) * 50000, 1700000000000 + (ident % 300) * 1000, int(ident % 11 != 0), ident))
            new.execute('UPDATE rating_snapshots SET score=?,popularity=?,xiancao_count=? WHERE book_id=?',
                        (scores[ident % len(scores)], ident % 100, ident % 50, ident))
        if ids:
            new.execute("UPDATE books SET title=?,author=?,tag1=?,tag2=? WHERE id=?",
                        ('100%_星河行者', "O'Neil/中文? #", '奇幻/中文? #', '星际', ids[-1]))
            new.execute('UPDATE rating_snapshots SET score=0 WHERE book_id=?', (ids[-1],))
        new.commit()
        report = {'synthetic': True, 'source': str(source), 'database': str(output / 'library.db'),
                  'books': new.execute('SELECT count(*) FROM books').fetchone()[0],
                  'chapters': new.execute('SELECT count(*) FROM chapters').fetchone()[0],
                  'schema': new.execute('SELECT max(version) FROM schema_migrations').fetchone()[0],
                  'note': 'SQLite backup; varied catalog metadata, original text/index and first five books retained'}
(output / 'fixture-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
print(json.dumps(report, ensure_ascii=False))
