#!/usr/bin/env python3
import tempfile
import unittest
import zipfile
from pathlib import Path

from generate_registry import PLUGIN_METADATA, TARGETS, generate_registry


class RegistryGeneratorTest(unittest.TestCase):
    def make_archives(self, root: Path, version: str) -> None:
        for plugin in PLUGIN_METADATA:
            for goos, _, asset_arch in TARGETS:
                extension = {"linux": ".so", "darwin": ".dylib", "windows": ".dll"}[goos]
                archive = root / f"{plugin}_{version}_{goos}_{asset_arch}.zip"
                with zipfile.ZipFile(archive, "w") as output:
                    output.writestr(f"{plugin}{extension}", b"plugin")

    def test_generates_all_matrix_artifacts(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.3")
            registry = generate_registry("1.2.3", "v1.2.3", root)

        self.assertEqual(len(registry["plugins"]), 4)
        self.assertEqual(
            sum(len(plugin["install"]["artifacts"]) for plugin in registry["plugins"]),
            24,
        )
        for plugin in registry["plugins"]:
            self.assertEqual(plugin["version"], "1.2.3")
            for artifact in plugin["install"]["artifacts"]:
                self.assertEqual(len(artifact["sha256"]), 64)
                self.assertGreater(artifact["size"], 0)
                self.assertIn("/v1.2.3/", artifact["url"])

    def test_missing_archive_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.3")
            (root / "kilo-free_1.2.3_linux_arm64.zip").unlink()
            with self.assertRaises(ValueError):
                generate_registry("1.2.3", "v1.2.3", root)

    def test_invalid_tag_version_pair_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            with self.assertRaises(ValueError):
                generate_registry("1.2.3", "1.2.3", Path(temp))
            with self.assertRaises(ValueError):
                generate_registry("dev", "vdev", Path(temp))


if __name__ == "__main__":
    unittest.main()
