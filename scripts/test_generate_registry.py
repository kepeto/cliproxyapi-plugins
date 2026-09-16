from __future__ import annotations

import sys
import tempfile
import unittest
import zipfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from generate_registry import PLUGIN_METADATA, TARGETS, generate_registry


class RegistryGeneratorTest(unittest.TestCase):
    def make_archives(self, root: Path, version: str, plugins: tuple[str, ...] | None = None) -> None:
        for plugin in plugins or tuple(PLUGIN_METADATA):
            for goos, _, asset_arch in TARGETS:
                archive = root / f"{plugin}_{version}_{goos}_{asset_arch}.zip"
                with zipfile.ZipFile(archive, "w") as handle:
                    extension = {"linux": ".so", "darwin": ".dylib", "windows": ".dll"}[goos]
                    handle.writestr(f"{plugin}{extension}", b"plugin")

    def test_generates_complete_registry(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.3")
            registry = generate_registry("1.2.3", "v1.2.3", root)
        self.assertEqual(len(registry["plugins"]), len(PLUGIN_METADATA))
        self.assertTrue(all(len(plugin["install"]["artifacts"]) == len(TARGETS) for plugin in registry["plugins"]))
    def test_complete_registry_contains_one_release_snapshot(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.4")
            registry = generate_registry("1.2.4", "v1.2.4", root)
        self.assertEqual({plugin["version"] for plugin in registry["plugins"]}, {"1.2.4"})
        artifact_urls = [
            artifact["url"]
            for plugin in registry["plugins"]
            for artifact in plugin["install"]["artifacts"]
        ]
        self.assertTrue(all("/releases/download/v1.2.4/" in url for url in artifact_urls))
    def test_invalid_tag_version_pair_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            with self.assertRaises(ValueError):
                generate_registry("1.2.3", "opencode-free-v1.2.3", Path(temp))
            with self.assertRaises(ValueError):
                generate_registry("dev", "vdev", Path(temp))


if __name__ == "__main__":
    unittest.main()
