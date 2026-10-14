import hashlib
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

import release


class ReleaseTests(unittest.TestCase):
    def test_nginx_gate_only_changes_library_https(self):
        original = "# config\n" + "\n".join(
            f"server {{\n    listen {port};\n    server_name {host}.private.xuexikuaile.fun;\n    location / {{ return 200; }}\n}}"
            for host in ("auth", "portal", "library") for port in ("80", "443 ssl http2"))
        result = release.maintenance_config(original)
        added = f"    if (-f {release.MARKER.as_posix()}) {{ return 503; }}\n"
        self.assertEqual(result.replace(added, ""), original)
        self.assertEqual(result.count(added), 1)
        self.assertGreater(result.index(added), result.rfind("listen 443"))
        with self.assertRaises(ValueError):
            release.maintenance_config(result)
        with self.assertRaises(ValueError):
            release.maintenance_config(original.replace("library.private", "unknown.private"))

    def test_bundle_detects_tampering_and_extra_files(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            asset = root / "image.tar"
            asset.write_bytes(b"verified-image")
            manifest = {"format": "personal-library/release@1", "revision": "a" * 40,
                        "imageId": "sha256:" + "b" * 64, "configId": "sha256:" + "c" * 64,
                        "imageRef": "personal-library:release-" + "a" * 12,
                        "files": {"image.tar": hashlib.sha256(asset.read_bytes()).hexdigest()}}
            (root / "package.json").write_text(json.dumps(manifest), encoding="utf-8")
            self.assertEqual(release.verify_bundle(root), manifest)
            asset.write_bytes(b"changed-image")
            with self.assertRaises(ValueError):
                release.verify_bundle(root)
            asset.write_bytes(b"verified-image")
            (root / "unexpected.py").write_bytes(b"extra")
            with self.assertRaises(ValueError):
                release.verify_bundle(root)

    def test_manifest_uses_target_timestamp_and_rejects_changed_body(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            novels = root / "novels"
            novels.mkdir()
            body = novels / "test.TXT"
            body.write_text("第一章\n真实测试。", encoding="utf-8")
            db = root / "legacy.db"
            with closing(sqlite3.connect(db)) as conn:
                conn.execute("CREATE TABLE books(filePath TEXT, hasContent INTEGER)")
                conn.execute("INSERT INTO books VALUES('test.TXT',1)")
                conn.commit()
            evidence = {"complete": True, "files": [{"path": "test.TXT", "encoding": "utf-8", **release.fingerprint(body)}]}
            evidence["files"][0]["modifiedNs"] = "1"
            out = root / "manifest.json"
            release.prepare_manifest(evidence, db, novels, out)
            result = json.loads(out.read_text(encoding="utf-8"))["files"][0]
            self.assertEqual(result["modifiedNs"], str(body.stat().st_mtime_ns))
            self.assertNotIn("sha256", result)
            body.write_text("第二章\n更新内容。", encoding="utf-8")
            with self.assertRaises(ValueError):
                release.prepare_manifest(evidence, db, novels, root / "changed.json")
            self.assertFalse((root / "changed.json").exists())

    def test_no_legacy_rollback_after_public_writes(self):
        with patch.object(release, "run") as run:
            with self.assertRaises(RuntimeError):
                release.rollback({"publicWritesOpened": True, "oldStopped": True, "createdContainers": [release.NEW]}, "original")
            run.assert_not_called()

    def test_failed_container_creation_still_recovers_old_service(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            nginx, marker = root / "nginx", root / "marker"
            nginx.write_text("maintenance", encoding="utf-8")
            marker.write_text("pending", encoding="utf-8")
            state = {"oldStopped": True, "createdContainers": [release.NEW]}
            with patch.object(release, "NGINX", nginx), patch.object(release, "MARKER", marker), patch.object(release, "run") as run, patch.object(release, "output", return_value=""), patch.object(release, "wait_healthy") as healthy:
                release.rollback(state, "original")
                run.assert_any_call("docker", "start", release.OLD)
                healthy.assert_called_once_with(6870)
            self.assertEqual(nginx.read_text(encoding="utf-8"), "original")
            self.assertFalse(marker.exists())
            self.assertTrue(state["rolledBack"])


if __name__ == "__main__":
    unittest.main()
