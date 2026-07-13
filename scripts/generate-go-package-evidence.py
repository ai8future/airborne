#!/usr/bin/env python3
"""Emit a deterministic inventory of Go production-package test evidence."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


EXCLUDED_PATH_PARTS = {"vendor", ".git", ".omx", "dashboard", "markdown_svc"}


def production_packages(root: Path) -> list[Path]:
    packages: list[Path] = []
    for directory in sorted({path.parent for path in root.glob("{cmd,internal}/**/*.go")}):
        relative = directory.relative_to(root)
        if any(part in EXCLUDED_PATH_PARTS for part in relative.parts):
            continue
        sources = sorted(
            path.name for path in directory.glob("*.go") if not path.name.endswith("_test.go")
        )
        if sources:
            packages.append(directory)
    return packages


def inventory(root: Path) -> dict[str, object]:
    entries = []
    missing = []
    for directory in production_packages(root):
        relative = directory.relative_to(root).as_posix()
        sources = sorted(
            path.name for path in directory.glob("*.go") if not path.name.endswith("_test.go")
        )
        tests = sorted(path.name for path in directory.glob("*_test.go"))
        bootstrap = relative.startswith("cmd/") and sources == ["main.go"]
        evidence = "bootstrap-excluded" if bootstrap else "direct" if tests else "missing"
        entry = {
            "package": relative,
            "production_files": sources,
            "test_evidence": evidence,
            "evidence_files": tests,
        }
        entries.append(entry)
        if evidence == "missing":
            missing.append(relative)
    return {
        "schema_version": 1,
        "scope": "cmd and internal production packages; generated protobuf and vendored modules excluded",
        "packages": entries,
        "missing_direct_evidence": missing,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--strict", action="store_true", help="fail when a non-bootstrap package lacks local tests")
    args = parser.parse_args()
    data = inventory(args.root.resolve())
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(data, indent=2) + "\n")
    if args.strict and data["missing_direct_evidence"]:
        print("packages without direct test evidence:", ", ".join(data["missing_direct_evidence"]))
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
