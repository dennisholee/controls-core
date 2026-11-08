#!/usr/bin/env python3
"""Fail when example configs that must stay identical drift apart."""
from __future__ import annotations

import hashlib
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GROUPS = {
    "envoy configs": ["examples/*/envoy/envoy.yaml"],
    "Go app Dockerfiles": ["examples/*/app/Dockerfile"],
    "PIP configs (non-mTLS)": [
        "examples/accounts-read/pip/pip.yaml",
        "examples/payments-write/pip/pip.yaml",
        "examples/spring-accounts/pip/pip.yaml",
    ],
    "Go app modules (non-payments)": [
        "examples/accounts-read/app/go.mod",
        "examples/mtls-caller/app/go.mod",
        "examples/pip-outage/app/go.mod",
    ],
}


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    failed = False
    for label, patterns in GROUPS.items():
        files = sorted({path for pattern in patterns for path in ROOT.glob(pattern)})
        if len(files) < 2:
            continue
        groups: dict[str, list[Path]] = {}
        for path in files:
            groups.setdefault(digest(path), []).append(path)
        if len(groups) > 1:
            failed = True
            print(f"{label} have drifted:", file=sys.stderr)
            for paths in groups.values():
                print("  " + ", ".join(str(p.relative_to(ROOT)) for p in paths), file=sys.stderr)
    if failed:
        return 1
    print("examples-config-check OK: shared example families are identical")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
