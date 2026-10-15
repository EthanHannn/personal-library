"""Install a narrow source-pack command into the existing ECS ops wrapper."""

import argparse
from datetime import datetime, timezone
from pathlib import Path
import shutil


BRANCH = r'''  library-source-pack)
    if [ $# -ne 2 ] || [ ! -f "$2" ] || [ ! -f "${SCRIPT_DIR}/personal-library-source-pack.py" ]; then
      echo "用法: $(basename "$0") library-source-pack <已审阅的单源配置.json>" >&2
      exit 1
    fi
    pack_hash=$(sha256sum "$2" | cut -d' ' -f1)
    if [[ ! "$pack_hash" =~ ^[0-9a-f]{64}$ ]]; then
      echo "书源配置哈希无效" >&2
      exit 1
    fi
    upload_dir=$(remote 'umask 077; mktemp -d /tmp/personal-library-sources.XXXXXXXX')
    if [[ ! "$upload_dir" =~ ^/tmp/personal-library-sources\.[a-zA-Z0-9]{8}$ ]]; then
      echo "书源暂存目录创建失败" >&2
      exit 1
    fi
    scp "${SSH_OPTS[@]}" "${SCRIPT_DIR}/personal-library-source-pack.py" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/apply.py"
    scp "${SSH_OPTS[@]}" "$2" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/source.json"
    remote "python3 -B '${upload_dir}/apply.py' '${upload_dir}/source.json' '${pack_hash}'"
    ;;
'''


def install(wrapper):
    if wrapper.name != "aliyun-ecs-ops.sh":
        raise ValueError("Expected the existing ECS wrapper")
    original = wrapper.read_text(encoding="utf-8")
    if "  library-source-pack)" in original:
        raise ValueError("Source-pack command already exists; review before changing it")
    if original.count("  deploy-zhixuan)") != 1:
        raise ValueError("Unexpected wrapper layout")
    program = Path(__file__).with_name("apply_source_pack.py")
    target = wrapper.with_name("personal-library-source-pack.py")
    if target.exists() and target.read_bytes() != program.read_bytes():
        raise ValueError("An existing source-pack program differs; review before replacing it")
    updated = original.replace("  deploy-zhixuan)", BRANCH + "  deploy-zhixuan)", 1)
    updated = updated.replace("  deploy-zhixuan <镜像tar>",
                              "  library-source-pack <单源配置.json>  校验并安装新版书屋书源\n  deploy-zhixuan <镜像tar>", 1)
    backup = wrapper.with_name(wrapper.name + ".before-sources-" +
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
    print(f"Installed library-source-pack; wrapper backup: {backup_path}; program: {program_path}")
