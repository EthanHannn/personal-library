"""Add the fixed library-snapshot operation to the non-secret ECS wrapper."""

import argparse
from pathlib import Path


def install(path):
    if path.name != "aliyun-ecs-ops.sh":
        raise ValueError("Expected the existing aliyun-ecs-ops.sh wrapper")
    script = path.read_text(encoding="utf-8")
    if "  library-snapshot)" in script:
        raise ValueError("library-snapshot already exists; review it before updating")
    marker = "  backup-db)"
    if script.count(marker) != 1 or script.count("  backup-db                 ") != 1:
        raise ValueError("Unexpected ECS wrapper layout")
    collector = Path(__file__).with_name("capture_legacy.py").read_text(encoding="utf-8")
    branch = '''  library-snapshot)
    if [ $# -ne 2 ] || [[ "$2" != *.tar.gz ]] || [ -e "$2" ]; then
      echo "用法: $(basename "$0") library-snapshot <本地新建.tar.gz>" >&2
      exit 1
    fi
    umask 077
    # Fixed, reviewed collector: only the legacy library; no service changes.
    # noclobber prevents replacing an existing local archive even in a race.
    (set -C; remote 'nice -n 19 ionice -c 3 python3 -' > "$2" <<'LIBRARY_SNAPSHOT_PY'
''' + collector + '''LIBRARY_SNAPSHOT_PY
    )
    test -s "$2"
    echo "书屋一致性快照已保存；服务与原始数据未修改。"
    ;;
'''
    script = script.replace(marker, branch + marker, 1)
    script = script.replace("  backup-db                 ", "  library-snapshot <新tar.gz> 下载书屋一致性快照、封面与正文指纹（不停服）\n  backup-db                 ", 1)
    path.write_text(script, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("wrapper", type=Path)
    install(parser.parse_args().wrapper)
