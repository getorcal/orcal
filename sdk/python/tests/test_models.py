import dataclasses

from orcal import models


def test_sandbox_model_carries_the_wire_fields():
    fields = {f.name for f in dataclasses.fields(models.Sandbox)}
    for expected in ("id", "image", "state", "runtime", "resources", "created_at", "updated_at", "network"):
        assert expected in fields


def test_list_models_expose_a_cursor():
    for name in ("SandboxList", "ExecList", "SnapshotList", "EventList"):
        model = getattr(models, name)
        fields = {f.name for f in dataclasses.fields(model)}
        assert "items" in fields
        assert "next_cursor" in fields


def test_created_token_carries_the_plaintext():
    fields = {f.name for f in dataclasses.fields(models.CreatedToken)}
    assert "token" in fields


def test_error_body_is_present():
    assert dataclasses.is_dataclass(models.ErrorBody)
