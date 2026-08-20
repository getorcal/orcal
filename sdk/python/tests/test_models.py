import dataclasses

import pytest

from orcal import models
from orcal._build import build

BOUNDARY_MODELS = (
    models.Version,
    models.Sandbox,
    models.Exec,
    models.Snapshot,
    models.FileInfo,
    models.Token,
    models.CreatedToken,
    models.Event,
)


def declared(model):
    return {f.name: f for f in dataclasses.fields(model)}


def minimal_payload(model):
    return {name: None for name, f in declared(model).items() if f.default is dataclasses.MISSING}


@pytest.mark.parametrize("model", BOUNDARY_MODELS, ids=lambda m: m.__name__)
def test_build_drops_a_field_the_model_does_not_declare(model):
    payload = dict(minimal_payload(model), brand_new_field="x")
    instance = build(model, payload)
    assert not hasattr(instance, "brand_new_field")
    for name in minimal_payload(model):
        assert getattr(instance, name) is None


@pytest.mark.parametrize("model", BOUNDARY_MODELS, ids=lambda m: m.__name__)
def test_build_keeps_every_field_the_model_declares(model):
    payload = {name: name for name in declared(model)}
    instance = build(model, payload)
    for name in declared(model):
        assert getattr(instance, name) == name


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
