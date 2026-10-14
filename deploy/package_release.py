"""Package an already built image and verified private encoding evidence."""

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile


def command(*args):
    return subprocess.check_output(args, text=True).strip()


def package(root, output, evidence_path, image):
    if command("git", "status", "--porcelain"):
        raise ValueError("Commit the release source before packaging")
    revision = command("git", "rev-parse", "HEAD")
    if command("docker", "image", "inspect", "--format", '{{index .Config.Labels "org.opencontainers.image.revision"}}', image) != revision:
        raise ValueError("Build the image with the current committed revision")
    evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
    if not evidence["complete"] or evidence["issues"]:
        raise ValueError("The real-data encoding rehearsal must have passed")
    output.mkdir(parents=True, exist_ok=False)
    stage = output / "package"
    stage.mkdir()
    paths = ["deploy/release.py", "deploy/verify_production.py", "tools/m4/backup_restore.py",
             "tools/m4/capture_legacy.py", "tools/m4/prepare_legacy.py"]
    for name in paths:
        target = stage / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(root / name, target)
    (stage / "encoding-evidence.json").write_text(json.dumps({"complete": True, "files": evidence["files"]}, ensure_ascii=False), encoding="utf-8")
    image_ref = "personal-library:release-" + revision[:12]
    subprocess.run(["docker", "tag", image, image_ref], check=True)
    subprocess.run(["docker", "save", "--output", str(stage / "image.tar"), image_ref], check=True)
    with tarfile.open(stage / "image.tar") as saved:
        images = json.load(saved.extractfile("manifest.json"))
        if len(images) != 1 or image_ref not in images[0]["RepoTags"]:
            raise ValueError("Expected exactly one tagged release image")
        config_id = "sha256:" + hashlib.sha256(saved.extractfile(images[0]["Config"]).read()).hexdigest()
    manifest = {"format": "personal-library/release@1", "revision": revision,
                "imageId": command("docker", "image", "inspect", "--format", "{{.Id}}", image),
                "configId": config_id, "imageRef": image_ref, "files": {}}
    for path in sorted(stage.rglob("*")):
        if path.is_file():
            with path.open("rb") as stream:
                manifest["files"][path.relative_to(stage).as_posix()] = hashlib.file_digest(stream, "sha256").hexdigest()
    (stage / "package.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    archive = output / "release.tar.gz"
    with tarfile.open(archive, "w:gz", compresslevel=6) as tar:
        for path in sorted(stage.rglob("*")):
            if path.is_file():
                tar.add(path, arcname=path.relative_to(stage).as_posix(), recursive=False)
    print(json.dumps({"revision": revision, "imageId": manifest["imageId"], "archiveBytes": archive.stat().st_size}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--image", required=True)
    args = parser.parse_args()
    package(Path(__file__).resolve().parents[1], args.out.resolve(), args.evidence.resolve(), args.image)
