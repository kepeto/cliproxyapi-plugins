#!/usr/bin/env python3
"""Sync plugins.configs.<id>.store.version / release-tag in a CPA config.yaml.

Keeps the host's desired plugin version aligned with locally deployed
binaries so the plugin loader never silently skips a version mismatch.
Usage: sync_store_versions.py <config.yaml> <version> <id> [id...]
"""
import re
import sys


def main() -> int:
    if len(sys.argv) < 4:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    path, version = sys.argv[1], sys.argv[2]
    ids = set(sys.argv[3:])
    with open(path, encoding="utf-8") as f:
        lines = f.readlines()

    cur_id = None
    in_store = False
    changed = set()
    out = []
    for line in lines:
        # 4-space-indented keys under plugins.configs are plugin ids.
        m = re.match(r"^    (\S+):", line)
        if m:
            cur_id = m.group(1)
            in_store = False
        if cur_id in ids and re.match(r"^      store:\s*$", line):
            in_store = True
        elif cur_id in ids and re.match(r"^      \S", line):
            in_store = False
        if in_store and cur_id in ids:
            if re.match(r"^\s*version:", line):
                line = re.sub(r"(version:).*", rf"\1 {version}", line)
                changed.add(cur_id)
            elif re.match(r"^\s*release-tag:", line):
                line = re.sub(r"(release-tag:).*", rf"\1 v{version}", line)
        out.append(line)

    with open(path, "w", encoding="utf-8") as f:
        f.writelines(out)
    synced = sorted(changed)
    print(f"synced store version -> {version}: {', '.join(synced) if synced else '(no matching blocks)'}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
