import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "manage-release.sh"


class ManageReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "bin").mkdir()
        (self.root / "releases").mkdir()
        self.env = {
            **os.environ,
            "PATH": f"{self.root / 'bin'}:{os.environ['PATH']}",
            "JOB_TYPE": "postsubmit",
            "PULL_BASE_SHA": "test-sha",
            "KONFLUX_SA_TOKEN": "test-token",
            "KONFLUX_PROJECT": "test-tenant",
            "RELEASE_TIMEOUT_SECONDS": "2",
            "RELEASE_POLL_SECONDS": "1",
            "DRYRUNONLY": "",
            "TEST_ROOT": str(self.root),
        }
        self.files = []
        self.write_command("curl", '''
import os
from pathlib import Path
print((Path(os.environ["TEST_ROOT"]) / "changes.json").read_text())
''')
        self.write_command("oc", '''
import json
import os
from pathlib import Path
import sys

root = Path(os.environ["TEST_ROOT"])
args = sys.argv[1:]
if args[:1] == ["--kubeconfig"]:
    args = args[2:]
if args[:1] == ["--namespace"]:
    args = args[2:]
with (root / "calls.jsonl").open("a") as log:
    log.write(json.dumps(args) + "\\n")

if args[0] in ("login", "project"):
    pass
elif args[:2] == ["auth", "can-i"]:
    print(os.environ.get("CAN_CREATE", "yes"))
elif args[:2] == ["get", "release"]:
    name = args[2]
    if "--ignore-not-found" in args:
        if os.environ.get("LOOKUP_ERROR"):
            sys.exit("lookup denied")
        if name == os.environ.get("EXISTING_NAME"):
            print("release/" + name)
    else:
        state_file = root / (name + ".polls")
        polls = int(state_file.read_text()) if state_file.exists() else 0
        state_file.write_text(str(polls + 1))
        reason = os.environ.get("RELEASE_REASON", "Succeeded")
        if polls == 0 and os.environ.get("PENDING_FIRST"):
            reason = "Progressing"
        print(json.dumps({"status": {"conditions": [{
            "type": "Released",
            "status": "True" if reason == "Succeeded" else "False",
            "reason": reason,
            "message": "test release status",
        }]}}))
elif args[0] == "create":
    manifest = json.loads((root / args[args.index("-f") + 1]).read_text())
    if "--dry-run=client" not in args and manifest["metadata"]["name"] == os.environ.get("CREATE_ERROR_NAME"):
        sys.exit("AlreadyExists")
    print(json.dumps(manifest))
else:
    sys.exit("unexpected oc call: " + repr(args))
''')

    def write_command(self, name, source):
        command = self.root / "bin" / name
        command.write_text("#!/usr/bin/env python3\n" + source)
        command.chmod(0o755)

    def release(self, name, catalog=False):
        path = f"releases/{name}.yaml"
        application = "lvm-operator-catalog" if catalog else "lvm-operator"
        manifest = {
            "apiVersion": "appstudio.redhat.com/v1alpha1",
            "kind": "Release",
            "metadata": {"name": name, "namespace": "test-tenant"},
            "spec": {
                "releasePlan": f"{application}-production-releaseplan-4-22",
                "snapshot": "test-snapshot",
            },
        }
        (self.root / path).write_text(json.dumps(manifest))
        self.files.append({"filename": path, "status": "added"})

    def run_script(self, **env):
        (self.root / "changes.json").write_text(json.dumps({"files": self.files}))

        result = subprocess.run(
            ["bash", str(SCRIPT)], cwd=self.root, env={**self.env, **env},
            text=True, capture_output=True, timeout=15,
        )

        calls_file = self.root / "calls.jsonl"
        self.calls = [json.loads(line) for line in calls_file.read_text().splitlines()] if calls_file.exists() else []
        return result

    def creates(self):
        return [call[call.index("-f") + 1] for call in self.calls if call[0] == "create" and "--dry-run=client" not in call]

    def test_existing_name_stops_all_creates(self):
        self.release("bundle")
        self.release("catalog", catalog=True)

        result = self.run_script(EXISTING_NAME="catalog")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exists", result.stdout + result.stderr)
        self.assertEqual(self.creates(), [])

    def test_bundle_succeeds_before_catalog_is_created(self):
        self.release("catalog", catalog=True)
        self.release("bundle")

        result = self.run_script(PENDING_FIRST="1")

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.creates(), ["releases/bundle.yaml", "releases/catalog.yaml"])
        polls = [i for i, call in enumerate(self.calls) if call[:3] == ["get", "release", "bundle"] and "--ignore-not-found" not in call]
        catalog_create = next(i for i, call in enumerate(self.calls) if call[0] == "create" and "--dry-run=client" not in call and "releases/catalog.yaml" in call)
        self.assertGreaterEqual(len(polls), 2)
        self.assertLess(polls[-1], catalog_create)

    def test_failed_bundle_blocks_catalog(self):
        self.release("catalog", catalog=True)
        self.release("bundle")

        result = self.run_script(RELEASE_REASON="Failed")

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.creates(), ["releases/bundle.yaml"])
        self.assertIn("test release status", result.stdout + result.stderr)

    def test_timeout_blocks_catalog(self):
        self.release("bundle")
        self.release("catalog", catalog=True)

        result = self.run_script(RELEASE_REASON="Progressing", RELEASE_TIMEOUT_SECONDS="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.creates(), ["releases/bundle.yaml"])
        self.assertIn("Timed out", result.stdout + result.stderr)

    def test_dry_run_checks_both_without_waiting(self):
        self.release("catalog", catalog=True)
        self.release("bundle")

        result = self.run_script(DRYRUNONLY="1")

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        creates = [call for call in self.calls if call[0] == "create" and "--dry-run=client" not in call]
        self.assertEqual(len(creates), 2)
        self.assertTrue(all("--dry-run=server" in call for call in creates))
        self.assertFalse(any(call[:2] == ["get", "release"] and "--ignore-not-found" not in call for call in self.calls))

    def test_catalog_only_still_runs(self):
        self.release("catalog", catalog=True)

        result = self.run_script()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.creates(), ["releases/catalog.yaml"])

    def test_all_bundles_finish_before_any_catalog(self):
        self.release("catalog", catalog=True)
        self.release("bundle-one")
        self.release("bundle-two")

        result = self.run_script()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.creates(), [
            "releases/bundle-one.yaml", "releases/bundle-two.yaml", "releases/catalog.yaml",
        ])
        catalog_create = next(i for i, call in enumerate(self.calls) if call[0] == "create" and "--dry-run=client" not in call and "releases/catalog.yaml" in call)
        for name in ("bundle-one", "bundle-two"):
            poll = next(i for i, call in enumerate(self.calls) if call[:3] == ["get", "release", name] and "--ignore-not-found" not in call)
            self.assertLess(poll, catalog_create)

    def test_presubmit_uses_create_with_server_dry_run(self):
        self.release("bundle")
        self.write_command("curl", '''
import json
import os
from pathlib import Path
data = json.loads((Path(os.environ["TEST_ROOT"]) / "changes.json").read_text())
print(json.dumps(data["files"]))
''')

        result = self.run_script(JOB_TYPE="presubmit", DRYRUNONLY="1")

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.creates(), ["releases/bundle.yaml"])

    def test_duplicate_batch_names_stop_creates(self):
        self.release("bundle")
        self.release("catalog", catalog=True)
        path = self.root / "releases/catalog.yaml"
        manifest = json.loads(path.read_text())
        manifest["metadata"]["name"] = "bundle"
        path.write_text(json.dumps(manifest))

        result = self.run_script()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("more than one", result.stdout + result.stderr)
        self.assertEqual(self.creates(), [])

    def test_wrong_namespace_stops_creates(self):
        self.release("bundle")
        path = self.root / "releases/bundle.yaml"
        manifest = json.loads(path.read_text())
        manifest["metadata"]["namespace"] = "other-tenant"
        path.write_text(json.dumps(manifest))

        result = self.run_script()

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.creates(), [])

    def test_removed_release_is_ignored(self):
        self.files.append({"filename": "releases/removed.yaml", "status": "removed"})

        result = self.run_script()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls, [])

    def test_lookup_error_stops_creates(self):
        self.release("bundle")

        result = self.run_script(LOOKUP_ERROR="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.creates(), [])

    def test_no_create_permission_stops_before_lookup(self):
        self.release("bundle")

        result = self.run_script(CAN_CREATE="no")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not have permission", result.stdout + result.stderr)
        self.assertFalse(any(call[0] == "get" for call in self.calls))

    def test_create_name_race_stops_catalog(self):
        self.release("bundle")
        self.release("catalog", catalog=True)

        result = self.run_script(CREATE_ERROR_NAME="bundle")

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.creates(), ["releases/bundle.yaml"])

    def test_no_release_changes_skips_cluster(self):
        self.files.append({"filename": "README.md", "status": "modified"})

        result = self.run_script()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
