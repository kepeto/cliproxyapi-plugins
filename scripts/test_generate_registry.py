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

    def test_generates_selected_plugin_registry(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.3", ("opencode-free",))
            registry = generate_registry("1.2.3", "opencode-free-v1.2.3", root, ("opencode-free",))
        self.assertEqual([plugin["id"] for plugin in registry["plugins"]], ["opencode-free"])
        self.assertEqual(len(registry["plugins"][0]["install"]["artifacts"]), 6)
        self.assertTrue(all(len(artifact["sha256"]) == 64 for artifact in registry["plugins"][0]["install"]["artifacts"]))

    def test_selected_plugin_preserves_other_registry_entries(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.make_archives(root, "1.2.4", ("opencode-free",))
            existing = root / "registry.json"
            existing.write_text(
                __import__("json").dumps({
                    "schema_version": 1,
                    "plugins": [
                        {"id": "nous-portal", "version": "1.2.3"},
                        {"id": "nous-portal-free", "version": "1.2.3"},
                        {"id": "opencode-free", "version": "1.2.3"},
                        {"id": "kilo-free", "version": "1.2.3"},
                    ],
                }),
                encoding="utf-8",
            )
            registry = generate_registry("1.2.4", "opencode-free-v1.2.4", root, ("opencode-free",), existing)
        versions = {plugin["id"]: plugin["version"] for plugin in registry["plugins"]}
        self.assertEqual(versions["opencode-free"], "1.2.4")
        self.assertEqual(versions["nous-portal"], "1.2.3")

    def test_invalid_tag_version_pair_fails(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            with self.assertRaises(ValueError):
                generate_registry("1.2.3", "v1.2.3", Path(temp))
            with self.assertRaises(ValueError):
                generate_registry("dev", "opencode-free-vdev", Path(temp))


if __name__ == "__main__":
    unittest.main()
