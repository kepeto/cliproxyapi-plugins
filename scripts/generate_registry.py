#!/usr/bin/env python3
"""Generate the plugin registry from the release archive set."""

from __future__ import annotations

import argparse
import hashlib
import json
import zipfile
from pathlib import Path


def _ascii_identifier(value: str) -> bool:
    return bool(value) and all(
        ("0" <= char <= "9")
        or ("A" <= char <= "Z")
        or ("a" <= char <= "z")
        or char == "-"
        for char in value
    )


def valid_version(version: str) -> bool:
    base, separator, build = version.partition("+")
    if separator and (not build or any(not _ascii_identifier(part) for part in build.split("."))):
        return False
    core, separator, prerelease = base.partition("-")
    if separator:
        if not prerelease:
            return False
        for part in prerelease.split("."):
            if not _ascii_identifier(part) or (part.isdigit() and len(part) > 1 and part[0] == "0"):
                return False
    parts = core.split(".")
    return len(parts) == 3 and all(
        part.isdigit() and (len(part) == 1 or part[0] != "0") for part in parts
    )


TARGETS = (
    ("linux", "amd64", "amd64"),
    ("linux", "arm64", "arm64"),
    ("linux", "arm", "arm"),
    ("darwin", "amd64", "amd64"),
    ("darwin", "arm64", "arm64"),
    ("windows", "amd64", "amd64"),
)
PLUGIN_METADATA = {
    "nous-portal": {
        "name": "Nous Portal",
        "description": "OAuth device-code authentication for Nous Portal inference API",
        "logo": "https://hermes-agent.nousresearch.com/favicon.ico",
        "homepage": "https://portal.nousresearch.com",
        "tags": ["nous", "portal", "oauth", "inference"],
    },
    "nous-portal-free": {
        "name": "Nous Portal Free",
        "description": "Nous Portal free models plugin for CLIProxyAPI",
        "logo": "https://hermes-agent.nousresearch.com/favicon.ico",
        "homepage": "https://portal.nousresearch.com",
        "tags": ["nous", "portal", "free", "oauth"],
    },
    "opencode-free": {
        "name": "OpenCode Zen Free",
        "description": "OpenCode Zen free models plugin for CLIProxyAPI",
        "logo": "https://opencode.ai/favicon.ico",
        "homepage": "https://opencode.ai",
        "tags": ["opencode", "free", "zen"],
    },
    "kilo-free": {
        "name": "KiloCode Free",
        "description": "KiloCode free models plugin for CLIProxyAPI",
        "logo": "https://kilo.ai/favicon.ico",
        "homepage": "https://kilo.ai",
        "tags": ["kilo", "free", "kilo-code"],
    },
}


def archive_info(dist: Path, plugin: str, version: str, tag: str) -> list[dict[str, object]]:
    artifacts: list[dict[str, object]] = []
    for goos, goarch, asset_arch in TARGETS:
        filename = f"{plugin}_{version}_{goos}_{asset_arch}.zip"
        path = dist / filename
        if not path.is_file() or path.stat().st_size == 0:
            raise ValueError(f"missing or empty release artifact: {path}")

        extension = {"linux": ".so", "darwin": ".dylib", "windows": ".dll"}[goos]
        try:
            with zipfile.ZipFile(path) as archive:
                members = {Path(name).name for name in archive.namelist()}
        except zipfile.BadZipFile as exc:
            raise ValueError(f"invalid release archive: {path}") from exc
        if f"{plugin}{extension}" not in members:
            raise ValueError(f"archive missing {plugin}{extension}: {path}")

        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        artifacts.append(
            {
                "goos": goos,
                "goarch": goarch,
                "url": f"https://github.com/kepeto/cliproxyapi-plugins/releases/download/{tag}/{filename}",
                "sha256": digest,
                "size": path.stat().st_size,
            }
        )
    return artifacts


def generate_registry(version: str, tag: str, dist: Path) -> dict[str, object]:
    if not valid_version(version):
        raise ValueError(f"invalid release version: {version!r}")
    if tag != f"v{version}":
        raise ValueError(f"release tag {tag!r} does not match version {version!r}")

    expected = {
        f"{plugin}_{version}_{goos}_{asset_arch}.zip"
        for plugin in PLUGIN_METADATA
        for goos, _, asset_arch in TARGETS
    }
    actual = {path.name for path in dist.glob("*.zip")}
    if actual != expected:
        missing = sorted(expected - actual)
        unexpected = sorted(actual - expected)
        raise ValueError(f"release artifact set mismatch: missing={missing}, unexpected={unexpected}")

    plugins = []
    for plugin, metadata in PLUGIN_METADATA.items():
        plugins.append(
            {
                "id": plugin,
                "name": metadata["name"],
                "description": metadata["description"],
                "author": "kepeto",
                "version": version,
                "repository": "https://github.com/kepeto/cliproxyapi-plugins",
                "logo": metadata["logo"],
                "homepage": metadata["homepage"],
                "license": "MIT",
                "tags": metadata["tags"],
                "install": {
                    "type": "github-release",
                    "artifacts": archive_info(dist, plugin, version, tag),
                },
            }
        )
    return {"schema_version": 1, "plugins": plugins}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--dist", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    registry = generate_registry(args.version, args.tag, args.dist)
    args.output.write_text(json.dumps(registry, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
