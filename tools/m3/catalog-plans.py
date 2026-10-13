"""Record metadata-only query plans on an existing synthetic business database."""
import argparse
import json
import re
import sqlite3
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('--db', required=True)
parser.add_argument('--out', required=True)
args = parser.parse_args()
db = sqlite3.connect(Path(args.db).resolve().as_uri() + '?mode=ro', uri=True)
if db.execute('SELECT title FROM books WHERE id=5').fetchone() != ('合成作品 5',):
    raise SystemExit('Synthetic fixture required')
source = (Path(__file__).resolve().parents[2] / 'internal/storage/queries.sql').read_text(encoding='utf-8')
params = {'search': '合', 'author': '', 'tag': '', 'tag1': '', 'tag2': '', 'min_score': 0, 'max_score': 10,
          'min_words': 0, 'max_words': 9007199254740991, 'only_content': 0, 'sort_by': 'score', 'page_limit': 24, 'page_offset': 4800}
report = {'synthetic': True, 'sqliteVersion': sqlite3.sqlite_version, 'queries': {},
          'note': 'Python SQLite query plans; API performance measured separately on Go release image'}
for name in ['CatalogStats', 'CatalogPage', 'CatalogCategories']:
    sql = source.split('-- name: ' + name + ' :')[1].split('\n', 1)[1].split(';', 1)[0]
    sql = re.sub(r'sqlc.arg\((\w+)\)', r':\1', sql)
    report['queries'][name] = {'sql': sql, 'parameters': params, 'plan': db.execute('EXPLAIN QUERY PLAN ' + sql, params).fetchall()}
    assert 'chapters' not in sql and 'book_contents' not in sql
with Path(args.out).open('x', encoding='utf-8') as handle:
    json.dump(report, handle, ensure_ascii=False, indent=2)
    handle.write('\n')
print(json.dumps({'complete': True, 'queries': len(report['queries'])}))
