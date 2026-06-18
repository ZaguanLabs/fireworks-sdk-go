# Fireworks Python SDK alpha.82 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.82`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.82/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.82 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `client.py` LoRA alpha defaults | `training/sdk/service_client.go`, `training/sdk/training_registry.go` | ported | LoRA training clients default `lora_alpha` to `32`, accept explicit alpha overrides, clear alpha for full-parameter clients, and include alpha in duplicate-client registry keys. |
| `managed.py` LoRA alpha config | `training/sdk/managed.go`, `training/sdk/managed_lifecycle.go`, `training/sdk/client_helpers.go` | ported | Managed provisioning config accepts `lora_alpha`, defaults it for LoRA handles, stores it on handles, and passes handle config through when constructing the managed training client. |
| `sampling.py` deployment sampler defaults | `training/sdk/sampling.go` | ported | Deployment sampler completion payloads include default `top_p: 1.0` and `top_k: 0` unless callers provide explicit values. |
| `patches/_tinker_lora_alpha_patch.py` | Go-native explicit fields | go-native | Python mutates Pydantic schemas at runtime; Go exposes `LoraAlpha` fields and constructor options directly. |
| Shared DPO loss config types | `types/generated.go`, `types/aliases.go` | ported | Generated `DpoConfig` support is wired into shared and params reinforcement-learning loss configs. |

## Training SDK

The alpha.80 training matrix remains valid for unchanged modules. Alpha.82 changed LoRA alpha handling, managed config propagation, deployment sampler request defaults, and shared DPO loss-config typing; those deltas are covered above and by focused Go tests.

## Broader SDK resources

Generated top-level resources are unchanged from alpha.80. The checked-in alpha.82 resource/type report at `docs/resource-type-parity-alpha82.md` has no missing Go resource methods or expected type names.

## Python-only behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, context-aware methods, futures, close methods, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.82 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.82/src` to alpha.80. Resource methods remained covered, generated types gained shared DPO config support, and the training SDK deltas are covered by tests for LoRA alpha defaults/overrides, registry key distinction, managed propagation, and sampler defaults.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
