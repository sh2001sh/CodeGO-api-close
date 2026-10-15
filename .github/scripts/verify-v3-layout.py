#!/usr/bin/env python3
"""Reject removed application code and accidental operational artifacts."""
import pathlib
import re
import subprocess


def main():
    root = pathlib.Path(__file__).resolve().parents[2]
    paths = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).decode("utf-8").split("\0")
    forbidden = (
        "cmd/", "internal/", "constant/", "dto/", "types/", "setting/", "i18n/",
        "web/default/", "electron/", "output/", ".playwright-cli/",
    )
    errors = []
    for path in paths:
        if not path or not (root / path).is_file():
            continue
        if path.startswith(forbidden) or path in ("go.mod", "go.sum", "docker-compose.v2.yml"):
            errors.append("removed V2 application or artifact remains tracked: " + path)
        if path.startswith("v3/") and path.endswith(".go"):
            source = (root / path).read_text(encoding="utf-8")
            for package in re.findall(r'"(github\.com/sh2001sh/new-api[^\"]*)"', source):
                if not package.startswith("github.com/sh2001sh/new-api/v3/"):
                    errors.append("V3 source imports the removed application: " + path)
    module = (root / "v3/go.mod").read_text(encoding="utf-8")
    if not module.startswith("module github.com/sh2001sh/new-api/v3\n"):
        errors.append("unexpected V3 module identity")
    if not (root / "web/app/package.json").is_file():
        errors.append("V3 frontend is missing")
    if errors:
        raise SystemExit("\n".join(errors))
    print("V3 repository layout verified; removed V2 application and operational artifacts are absent")


if __name__ == "__main__":
    main()
