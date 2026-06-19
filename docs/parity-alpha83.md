# Fireworks Python SDK alpha.83 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.83`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.83/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.83 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `pyproject.toml` version source + `_version.py` metadata fallback | `version.go`, `client_test.go` | ported | Go keeps an explicit package version constant aligned to the Python release version. |
| `patches/_tinker_r3_patch.py` wire model rebuild | `training/sdk/forward_backward.go`, `training/sdk/forward_backward_test.go` | go-native | Python rebuilds Tinker's cached Pydantic wire serializers so `routing_matrices` survive request conversion. Go serializes `TrainingDatum.ModelInput` maps directly, with a focused nested wire-payload test covering both forward and forward-backward shapes. |

## Training SDK

The alpha.82 training matrix remains valid for unchanged modules. Alpha.83 only changed Python version-source handling and the Tinker R3 runtime patch; the Go port covers those with an explicit version constant and direct JSON request serialization.

## Broader SDK resources

Generated top-level resources and type definitions are unchanged from alpha.82. The checked-in alpha.83 resource/type report at `docs/resource-type-parity-alpha83.md` has no missing Go resource methods or expected type names.

## Python-only behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation, rebuild hooks, and cached internal serializer refresh.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, context-aware methods, futures, close methods, direct JSON payloads, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.83 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.83/src` to alpha.82. Resource methods and generated types remained covered. The only training behavior delta is Python-specific cached serializer rebuilding for R3 routing matrices; Go has no equivalent cache layer and preserves routing matrices through direct JSON serialization.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
