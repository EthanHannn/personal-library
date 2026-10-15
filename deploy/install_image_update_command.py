"""Add one guarded incremental image-update command to the ECS ops wrapper."""

import argparse
from datetime import datetime, timezone
from pathlib import Path
import shutil


BRANCH = r'''  library-image-update)
    if [ $# -ne 3 ] || [ ! -f "$2" ] || [ ! -f "${SCRIPT_DIR}/personal-library-update-image.py" ] || [[ ! "$3" =~ ^[0-9a-f]{40}$ ]]; then
      echo "用法: $(basename "$0") library-image-update <已审阅镜像tar> <完整commit>" >&2
      exit 1
    fi
    image_hash=$(sha256sum "$2" | cut -d' ' -f1)
    upload_dir=$(remote 'umask 077; mktemp -d /tmp/personal-library-image.XXXXXXXX')
    if [[ ! "$upload_dir" =~ ^/tmp/personal-library-image\.[a-zA-Z0-9]{8}$ ]]; then
      echo "镜像暂存目录创建失败" >&2
      exit 1
    fi
    scp "${SSH_OPTS[@]}" "${SCRIPT_DIR}/personal-library-update-image.py" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/update.py"
    scp "${SSH_OPTS[@]}" "$2" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/image.tar"
    remote "python3 -B '${upload_dir}/update.py' '${upload_dir}/image.tar' '${image_hash}' '$3'"
    ;;
'''


def install(wrapper):
    if wrapper.name != "aliyun-ecs-ops.sh":
        raise ValueError("Expected the existing ECS wrapper")
    original = wrapper.read_text(encoding="utf-8")
    if "  library-image-update)" in original:
        raise ValueError("Image-update command already exists; review before changing it")
    if original.count("  library-source-pack)") != 1:
        raise ValueError("Expected installed source-pack command")
    program = Path(__file__).with_name("update_image.py")
    target = wrapper.with_name("personal-library-update-image.py")
    if target.exists() and target.read_bytes() != program.read_bytes():
        raise ValueError("Existing image-update program differs; review before replacing it")
    updated = original.replace("  library-source-pack)", BRANCH + "  library-source-pack)", 1)
    updated = updated.replace("  library-source-pack <单源配置.json>",
                              "  library-image-update <镜像tar> <完整commit>  回滚保护下更新新版书屋镜像\n  library-source-pack <单源配置.json>", 1)
    backup = wrapper.with_name(wrapper.name + ".before-image-update-" +
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
    print(f"Installed library-image-update; wrapper backup: {backup_path}; program: {program_path}")
