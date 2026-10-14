"""Copy a synthetic business DB and seed votes already included in its totals."""
import argparse
import json
import sqlite3
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('--source', required=True)
parser.add_argument('--output', required=True)
args = parser.parse_args()
source, output = Path(args.source).resolve(), Path(args.output).resolve()
if output.exists():
    raise SystemExit('Output must not exist; choose an independent rehearsal directory')
with sqlite3.connect(source.as_uri() + '?mode=ro', uri=True) as old:
    if old.execute('SELECT title FROM books WHERE id=5').fetchone() != ('合成作品 5',):
        raise SystemExit('Synthetic M0-derived business fixture required')
    output.mkdir(parents=True)
    with sqlite3.connect(output / 'library.db') as new:
        old.backup(new)
        new.execute('PRAGMA foreign_keys=ON')
        new.execute("INSERT INTO readers VALUES('vote-history-reader','vote-history-reader',NULL,'USER',NULL,123,123)")
        new.execute('UPDATE rating_snapshots SET popularity=100,total_score=884,score=8.8,xiancao_count=70,liangcao_count=20,gancao_count=8,kucao_count=1,ducao_count=1 WHERE book_id=5')
        new.execute("INSERT INTO votes VALUES('imported-owner-vote','XIANCAO','XIANCAO','default-reader',5,123)")
        new.execute("INSERT INTO votes VALUES('imported-history-vote','DUCAO','DUCAO','vote-history-reader',5,456)")
        new.commit()
        report = {'synthetic': True, 'database': str(output / 'library.db'),
                  'books': new.execute('SELECT count(*) FROM books').fetchone()[0],
                  'chapters': new.execute('SELECT count(*) FROM chapters').fetchone()[0],
                  'schema': new.execute('SELECT max(version) FROM schema_migrations').fetchone()[0],
                  'note': 'Independent backup; book 5 has two imported votes included in 100 historical votes'}
(output / 'fixture-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
print(json.dumps(report))
