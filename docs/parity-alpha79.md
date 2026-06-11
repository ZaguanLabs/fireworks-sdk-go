# Fireworks Python SDK alpha.79 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.79`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.79/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.79 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `sampling.py` Tinker response construction | `training/sdk/sampling.go` | ported | `FiretitanSampleResponse` and `FiretitanSampledSequence` keep idiomatic Go fields while marshaling to Tinker-compatible `_prompt_logprobs_list`, `_tokens_list`, and `_logprobs_list` names introduced upstream. |
| `test_firetitan_tinker_compat.py` shape expectations | `training/sdk/sampling_test.go` | ported | Added focused JSON shape assertions for alpha.79 sampling responses, including prompt logprobs with leading `null`. |

## Training SDK

The alpha.78 training matrix remains valid for unchanged modules. Alpha.79 only changed `sampling.py`; the delta is covered above and by focused Go tests.

## Broader SDK resources

Generated top-level resources and types are unchanged from alpha.78. The checked-in alpha.79 resource/type report at `docs/resource-type-parity-alpha79.md` has no missing Go resource methods or expected type names.

## Python-only behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, context-aware methods, futures, close methods, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.79 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.79/src` to alpha.78 and found no generated resource/type changes. The changed sampling compatibility shape is covered by unit tests and the alpha.79 resource/type parity report remains clean.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
