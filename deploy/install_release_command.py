"""Install the reviewed, fixed personal-library cutover in the ECS wrapper."""

import argparse
from pathlib import Path


BRANCH = r'''  deploy-personal-library)
    if [ $# -ne 2 ] || [ ! -f "$2" ]; then
      echo "用法: $(basename "$0") deploy-personal-library <已验证的发布包.tar.gz>" >&2
      exit 1
    fi
    upload_dir=$(remote 'umask 077; mktemp -d /tmp/personal-library-release.XXXXXXXX')
    if [[ ! "$upload_dir" =~ ^/tmp/personal-library-release\.[a-zA-Z0-9]{8}$ ]]; then
      echo "发布目录创建失败" >&2
      exit 1
    fi
    scp "${SSH_OPTS[@]}" "$2" "${SERVER_USER}@${SERVER_HOST}:${upload_dir}/release.tar.gz"
    remote "set -euo pipefail; python3 - '$upload_dir'" <<'PERSONAL_LIBRARY_START'
import pathlib, subprocess, sys, tarfile
from datetime import datetime, timezone
upload = pathlib.Path(sys.argv[1])
stage = upload / 'stage'
stage.mkdir(mode=0o700)
with tarfile.open(upload / 'release.tar.gz') as archive:
    members = archive.getmembers()
    names = set()
    total = 0
    for entry in members:
        relative = pathlib.PurePosixPath(entry.name)
        if (not entry.isfile() or relative.is_absolute() or '..' in relative.parts
                or '\\' in entry.name or ':' in entry.name or entry.name in names
                or not (stage / relative).resolve().is_relative_to(stage.resolve())):
            raise ValueError('Unsafe release archive entry')
        names.add(entry.name)
        total += entry.size
        if total > 512 * 1024 * 1024:
            raise ValueError('Release exceeds size budget')
    archive.extractall(stage, members=members)
unit = 'personal-library-release-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
subprocess.run(['systemd-run', '--unit=' + unit, '--collect', '--property=Type=exec',
                '--property=Nice=10', '--property=IOSchedulingClass=idle', '--property=UMask=0022',
                'python3', '-B', str(stage / 'deploy/release.py'), str(stage)], check=True)
print('Release started independently of this connection; journal unit: ' + unit)
PERSONAL_LIBRARY_START
    ;;
'''


def install(path):
    if path.name != "aliyun-ecs-ops.sh":
        raise ValueError("Expected the existing ECS wrapper")
    original = path.read_text(encoding="utf-8")
    if "  deploy-personal-library)" in original:
        raise ValueError("Release command already exists; review before changing it")
    if original.count("  deploy-zhixuan)") != 1:
        raise ValueError("Unexpected wrapper layout")
    updated = original.replace("  deploy-zhixuan)", BRANCH + "  deploy-zhixuan)", 1)
    updated = updated.replace("  deploy-zhixuan <镜像tar>", "  deploy-personal-library <发布包>  旧版书屋备份、迁移、验证与新版切换\n  deploy-zhixuan <镜像tar>", 1)
    path.write_text(updated, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("wrapper", type=Path)
    install(parser.parse_args().wrapper)
