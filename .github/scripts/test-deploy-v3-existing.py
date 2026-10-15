import copy
import importlib.util
import pathlib
import os
import json
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("deploy", pathlib.Path(__file__).with_name("deploy-v3-existing.py"))
deploy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deploy)


class DeploymentPlanTest(unittest.TestCase):
    def setUp(self):
        self.document = {
            "name": "existing-production",
            "services": {
                role: {
                    "image": deploy.REPOSITORY + ":sha-1234567-" + role + "-amd64",
                    "environment": {"V3_PG_DSN": "test-only", "V3_SECRET_KEY": "test-only"},
                    "volumes": ["existing-wal:/data/wal"],
                    "network_mode": "host",
                    "mem_limit": "4g",
                    "memswap_limit": "4g",
                    "command": ["-addr", "127.0.0.1:3400"],
                } for role in deploy.ROLES
            },
            "volumes": {"existing-wal": {"external": True}},
        }
        self.revision = "b" * 40

    def test_preserves_existing_configuration(self):
        original = copy.deepcopy(self.document)
        updated = deploy.prepare(self.document, self.revision)
        for role in deploy.ROLES:
            self.assertEqual(updated["services"][role]["image"], deploy.REPOSITORY + ":sha-bbbbbbb-" + role + "-amd64")
            updated["services"][role]["image"] = original["services"][role]["image"]
        self.assertEqual(updated, original)
        self.assertEqual(self.document, original)

    def test_removes_only_known_startup_override(self):
        for role in ("gateway", "worker"):
            with self.subTest(role=role):
                service = self.document["services"][role]
                target = "/opt/codego-startup-" + role
                service["entrypoint"] = [target]
                service["volumes"].append("/existing/startup-timeout-" + role + ":" + target + ":ro")
                updated = deploy.prepare(self.document, self.revision)["services"][role]
                self.assertNotIn("entrypoint", updated)
                self.assertEqual(updated["volumes"], ["existing-wal:/data/wal"])

    def test_rejects_unknown_entrypoint(self):
        for role in deploy.ROLES:
            with self.subTest(role=role):
                document = copy.deepcopy(self.document)
                document["services"][role]["entrypoint"] = ["/unknown/program"]
                with self.assertRaisesRegex(ValueError, "unknown entrypoint"):
                    deploy.prepare(document, self.revision)

    def test_removes_known_emergency_overrides_for_all_backend_roles(self):
        for role in ("gateway", "control", "worker"):
            with self.subTest(role=role):
                service = self.document["services"][role]
                target = "/opt/codego-emergency-" + role
                service["entrypoint"] = [target]
                service["volumes"].append({"type": "bind", "source": "/existing/emergency-ledger-" + role, "target": target, "read_only": True})
                updated = deploy.prepare(self.document, self.revision)["services"][role]
                self.assertNotIn("entrypoint", updated)
                self.assertEqual(updated["volumes"], ["existing-wal:/data/wal"])
                self.assertEqual(updated["mem_limit"], "4g")
                self.assertEqual(updated["memswap_limit"], "4g")

    def test_rejects_impostor_patch_source_or_writable_mount(self):
        for source, readonly in (("/existing/unknown-program", True), ("/existing/emergency-ledger-control", False), ("emergency-ledger-control", True)):
            with self.subTest(source=source, readonly=readonly):
                document = copy.deepcopy(self.document)
                service = document["services"]["control"]
                service["entrypoint"] = ["/opt/codego-emergency-control"]
                service["volumes"].append({"type": "bind", "source": source, "target": "/opt/codego-emergency-control", "read_only": readonly})
                with self.assertRaisesRegex(ValueError, "unknown entrypoint"):
                    deploy.prepare(document, self.revision)

    def test_rejects_executable_override_for_web(self):
        service = self.document["services"]["web"]
        service["entrypoint"] = ["/opt/codego-emergency-web"]
        service["volumes"].append("/existing/emergency-ledger-web:/opt/codego-emergency-web:ro")
        with self.assertRaisesRegex(ValueError, "unknown entrypoint"):
            deploy.prepare(self.document, self.revision)

    def test_rejects_missing_role_or_foreign_image(self):
        del self.document["services"]["worker"]
        with self.assertRaisesRegex(ValueError, "missing"):
            deploy.prepare(self.document, self.revision)
        self.document["services"]["worker"] = {"image": "unrelated:latest"}
        with self.assertRaisesRegex(ValueError, "unexpected image"):
            deploy.prepare(self.document, self.revision)

    def test_rejects_non_commit_reference(self):
        for value in ("main", "latest", "bbbbbbb", "B" * 40):
            with self.subTest(value=value), self.assertRaises(ValueError):
                deploy.prepare(self.document, value)

    def exercise_runtime(self, fail_after_replace=False, wrong_revision=False):
        previous_directory = pathlib.Path.cwd()
        self.addCleanup(os.chdir, previous_directory)
        with tempfile.TemporaryDirectory() as temporary:
            path = pathlib.Path(temporary) / "compose.json"
            original = json.dumps(self.document)
            path.write_text(original, encoding="utf-8")
            calls = []

            def docker(args, **kwargs):
                calls.append(args)
                if fail_after_replace and "gateway" in args and "up" in args and not getattr(docker, "failed", False):
                    docker.failed = True
                    raise subprocess.CalledProcessError(1, args)

            inspection = json.dumps([{"Config": {"Labels": {"org.opencontainers.image.revision": "a" * 40 if wrong_revision else self.revision}}}])
            with mock.patch.dict(os.environ, {"DEPLOY_COMMIT_SHA": self.revision, "DEPLOY_PATH": temporary, "DEPLOY_COMPOSE_FILE": "compose.json"}), mock.patch.object(deploy, "run", side_effect=docker), mock.patch.object(deploy.subprocess, "check_output", return_value=inspection):
                if wrong_revision:
                    with self.assertRaisesRegex(ValueError, "revision does not match"):
                        deploy.main()
                    self.assertEqual(path.read_text(encoding="utf-8"), original)
                    self.assertFalse(any("up" in args for args in calls))
                elif fail_after_replace:
                    with self.assertRaises(subprocess.CalledProcessError):
                        deploy.main()
                    self.assertEqual(path.read_text(encoding="utf-8"), original)
                    self.assertEqual(calls[-1][-4:], list(deploy.ROLES))
                else:
                    deploy.main()
                    updated = json.loads(path.read_text(encoding="utf-8"))
                    self.assertEqual(updated, deploy.prepare(self.document, self.revision))
                    self.assertTrue(all("pull" in args for args in calls[:4]))
                    self.assertEqual(calls[-1][-1], "web")
            os.chdir(previous_directory)

    def test_runtime_preloads_images_before_replacing_services(self):
        self.exercise_runtime()

    def test_runtime_rolls_back_original_compose_after_startup_failure(self):
        self.exercise_runtime(fail_after_replace=True)

    def test_runtime_rejects_mismatched_image_before_touching_production(self):
        self.exercise_runtime(wrong_revision=True)


if __name__ == "__main__":
    unittest.main()
