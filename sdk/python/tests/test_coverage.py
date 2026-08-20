import pathlib

import yaml

import orcal
from orcal.client import Orcal
from orcal.sandbox import Sandbox, SandboxFiles
from orcal.snapshot import Snapshot

SPEC = pathlib.Path(__file__).resolve().parents[3] / "spec" / "openapi.yaml"

COVERED_BY = {
    ("get", "/healthz"): "Orcal.healthz",
    ("get", "/version"): "Orcal.version",
    ("get", "/sandboxes"): "Orcal.sandboxes",
    ("post", "/sandboxes"): "Orcal.sandbox",
    ("get", "/sandboxes/{ref}"): "Orcal.get_sandbox",
    ("delete", "/sandboxes/{ref}"): "Sandbox.destroy",
    ("post", "/sandboxes/{ref}/start"): "Sandbox.start",
    ("post", "/sandboxes/{ref}/stop"): "Sandbox.stop",
    ("get", "/sandboxes/{ref}/execs"): "Sandbox.execs",
    ("post", "/sandboxes/{ref}/execs"): "Sandbox.exec",
    ("get", "/execs/{id}"): "Orcal.get_exec",
    ("get", "/execs/{id}/output"): "ExecStream.__iter__",
    ("get", "/sandboxes/{ref}/snapshots"): "Sandbox.snapshots",
    ("post", "/sandboxes/{ref}/snapshots"): "Sandbox.snapshot",
    ("post", "/sandboxes/{ref}/restore"): "Sandbox.restore",
    ("get", "/snapshots"): "Orcal.snapshots",
    ("get", "/snapshots/{ref}"): "Orcal.get_snapshot",
    ("delete", "/snapshots/{ref}"): "Snapshot.delete",
    ("get", "/sandboxes/{ref}/files"): "SandboxFiles.read",
    ("put", "/sandboxes/{ref}/files"): "SandboxFiles.write",
    ("get", "/sandboxes/{ref}/files/stat"): "SandboxFiles.stat",
    ("get", "/sandboxes/{ref}/files/list"): "SandboxFiles.list",
    ("get", "/sandboxes/{ref}/archive"): "SandboxFiles.download",
    ("put", "/sandboxes/{ref}/archive"): "SandboxFiles.upload",
    ("get", "/tokens"): "Orcal.list_tokens",
    ("post", "/tokens"): "Orcal.create_token",
    ("delete", "/tokens/{id}"): "Orcal.revoke_token",
    ("get", "/events"): "Orcal.events",
}

METHODS = {"get", "post", "put", "delete", "patch"}


def spec_operations():
    doc = yaml.safe_load(SPEC.read_text())
    assert doc["paths"], "parsed zero paths; the spec is not being read"
    return {(m, p) for p, ops in doc["paths"].items() for m in ops if m in METHODS}


def test_every_operation_has_a_client_method():
    missing = spec_operations() - set(COVERED_BY)
    assert not missing, f"operations with no SDK method: {sorted(missing)}"


def test_no_stale_entries_in_the_coverage_table():
    stale = set(COVERED_BY) - spec_operations()
    assert not stale, f"coverage table names operations the spec no longer has: {sorted(stale)}"


def test_every_named_method_exists():
    owners = {
        "Orcal": Orcal,
        "Sandbox": Sandbox,
        "SandboxFiles": SandboxFiles,
        "Snapshot": Snapshot,
        "ExecStream": orcal.ExecStream,
    }
    for operation, dotted in COVERED_BY.items():
        owner, _, attribute = dotted.partition(".")
        assert hasattr(owners[owner], attribute), f"{dotted} does not exist, but {operation} claims it"
