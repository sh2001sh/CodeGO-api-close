#!/usr/bin/env python3
"""Update images in an existing V3 JSON Compose without resetting its topology."""
import copy
import json
import os
import pathlib
import re
import subprocess
import tempfile

ROLES = ("worker", "gateway", "control", "web")
REPOSITORY = "ghcr.io/sh2001sh/codego-api"


def prepare(document, revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("an exact lowercase 40-character Git commit is required")
    result = copy.deepcopy(document)
    services = result.get("services", {})
    for role in ROLES:
        if role not in services:
            raise ValueError("existing V3 service is missing: " + role)
        service = services[role]
        image = service.get("image", "")
        if not image.startswith(REPOSITORY + ":") or not image.endswith("-" + role + "-amd64"):
            raise ValueError("refusing to replace an unexpected image for " + role)
        patched_targets = []
        volumes = []
        known_patches = {}
        if role in ("gateway", "worker"):
            known_patches["/opt/codego-startup-" + role] = "startup-timeout-" + role
        if role in ("gateway", "control", "worker"):
            known_patches["/opt/codego-emergency-" + role] = "emergency-ledger-" + role
        for mount in service.get("volumes", []):
            if isinstance(mount, str):
                parts = mount.split(":")
                source, target = parts[0], parts[1] if len(parts) > 1 else ""
                readonly = len(parts) > 2 and "ro" in parts[2].split(",")
            else:
                source, target = mount.get("source", ""), mount.get("target", "")
                readonly = mount.get("read_only", False)
            source_path = pathlib.PurePosixPath(source)
            if readonly and source_path.is_absolute() and known_patches.get(target) == source_path.name:
                patched_targets.append(target)
            else:
                volumes.append(mount)
        entrypoint = service.get("entrypoint")
        entry_args = entrypoint if isinstance(entrypoint, list) else [entrypoint] if entrypoint else []
        if entry_args:
            if len(entry_args) != 1 or entry_args[0] not in patched_targets:
                raise ValueError("unknown entrypoint override for " + role)
            service.pop("entrypoint")
        if patched_targets:
            if any(not target or target == "/" for target in patched_targets):
                raise ValueError("invalid startup patch mount target")
            service["volumes"] = volumes
        if "build" in service:
            raise ValueError("existing production service unexpectedly has a build configuration")
        service["image"] = REPOSITORY + ":sha-" + revision[:7] + "-" + role + "-amd64"
    if "migrate" in services:
        image = services["migrate"].get("image", "")
        if image.startswith(REPOSITORY + ":") and image.endswith("-migrate-amd64"):
            services["migrate"]["image"] = REPOSITORY + ":sha-" + revision[:7] + "-migrate-amd64"
    return result


def run(args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)


def write_private(path, content):
    fd, staged = tempfile.mkstemp(prefix=".v3-compose-", suffix=".json", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(content)
        os.chmod(staged, 0o600)
        os.replace(staged, path)
    finally:
        if os.path.exists(staged):
            os.unlink(staged)


def main():
    revision = os.environ["DEPLOY_COMMIT_SHA"]
    directory = pathlib.Path(os.environ["DEPLOY_PATH"]).resolve(strict=True)
    filename = os.environ.get("DEPLOY_COMPOSE_FILE", "compose.json")
    path = (directory / filename).resolve(strict=True)
    if path.parent != directory or path.suffix != ".json":
        raise ValueError("deployment requires an existing JSON Compose in the selected directory")
    original = path.read_text(encoding="utf-8")
    candidate = prepare(json.loads(original), revision)
    os.chdir(directory)
    # Pull and inspect every image before replacing any production container.
    for role in ROLES:
        image = candidate["services"][role]["image"]
        run(["docker", "pull", image], stdout=subprocess.DEVNULL)
        inspected = json.loads(subprocess.check_output(["docker", "image", "inspect", image], text=True))[0]
        actual = (inspected.get("Config", {}).get("Labels") or {}).get("org.opencontainers.image.revision")
        if actual != revision:
            raise ValueError("image revision does not match the verified commit: " + role)
    write_private(path, json.dumps(candidate, indent=2) + "\n")
    command = ["docker", "compose", "-f", str(path)]
    try:
        run(command + ["config", "--quiet"])
        run(command + ["up", "-d", "--no-deps", "--wait", "--wait-timeout", "900", "worker"])
        run(command + ["up", "-d", "--no-deps", "--wait", "--wait-timeout", "900", "gateway", "control"])
        # Recreate the edge after backends so its upstream DNS cannot be stale.
        run(command + ["up", "-d", "--no-deps", "--force-recreate", "--wait", "--wait-timeout", "180", "web"])
    except BaseException:
        write_private(path, original)
        # Restore the original image references and original startup overrides.
        try:
            run(command + ["up", "-d", "--no-deps", "--wait", "--wait-timeout", "900", *ROLES])
        except Exception as rollback_error:
            raise RuntimeError("V3 deployment failed and original runtime could not be restored") from rollback_error
        raise
    print("V3 deployment completed at commit " + revision)


if __name__ == "__main__":
    main()
