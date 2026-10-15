"""Add a search-only production check to the ECS ops wrapper."""

import argparse
from datetime import datetime, timezone
from pathlib import Path
import shutil


BRANCH = r'''  library-source-query)
    if [ $# -ne 3 ] || [ ! -f "$2" ] || [ -z "$3" ] || [ ! -f "${SCRIPT_DIR}/personal-library-source-pack.py" ] || [ ! -f "${SCRIPT_DIR}/personal-library-source-query.py" ]; then
      echo "用法: $(basename "$0") library-source-query <已审阅单源配置.json> <书名>" >&2
      exit 1
    fi
    pack_hash=$(sha256sum "$2" | cut -d' ' -f1)
    upload_dir=$(remote 'umask 077; mktemp -d /tmp/personal-library-query.XXXXXXXX')
    if [[ ! "$upload_dir" =~ ^/tmp/personal-library-query\.[a-zA-Z0-9]{8}$ ]]; then
      echo "查询暂存目录创建失败" >&2
      exit 1
    fi
    scp "${SSH_OPTS[@]}" "${SCRIPT_DIR}/personal-library-source-pack.py" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/apply_source_pack.py"
    scp "${SSH_OPTS[@]}" "${SCRIPT_DIR}/personal-library-source-query.py" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/query.py"
    scp "${SSH_OPTS[@]}" "$2" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/source.json"
    printf '%s' "$3" | remote "python3 -B '${upload_dir}/query.py' '${upload_dir}/source.json' '${pack_hash}'"
    ;;
'''


def install(wrapper):
    if wrapper.name != "aliyun-ecs-ops.sh":
        raise ValueError("Expected the existing ECS wrapper")
    original = wrapper.read_text(encoding="utf-8")
    if "  library-source-query)" in original:
        raise ValueError("Source query command already exists")
    if original.count("  library-source-pack)") != 1:
        raise ValueError("Expected installed source-pack command")
    program = Path(__file__).with_name("check_source_query.py")
    target = wrapper.with_name("personal-library-source-query.py")
    if target.exists() and target.read_bytes() != program.read_bytes():
        raise ValueError("An existing source-query helper differs")
    updated = original.replace("  library-source-pack)", BRANCH + "  library-source-pack)", 1)
    updated = updated.replace("  library-source-pack <单源配置.json>",
                              "  library-source-query <单源配置.json> <书名>  用已审阅书源执行一次线上搜索\n  library-source-pack <单源配置.json>", 1)
    backup = wrapper.with_name(wrapper.name + ".before-source-query-" +
                               datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ"))
    if backup.exists():
        raise ValueError("Backup path already exists")
    shutil.copy2(wrapper, backup)
    shutil.copy2(program, target)
    wrapper.write_text(updated, encoding="utf-8", newline="\n")
    return backup, target


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("wrapper", type=Path)
    backup_path, program_path = install(parser.parse_args().wrapper)
    print(f"Installed library-source-query; wrapper backup: {backup_path}; program: {program_path}")
