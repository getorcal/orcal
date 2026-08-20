import dataclasses
import functools


@functools.lru_cache(maxsize=None)
def _declared(model):
    return frozenset(f.name for f in dataclasses.fields(model))


def build(model, payload):
    declared = _declared(model)
    return model(**{key: value for key, value in payload.items() if key in declared})
