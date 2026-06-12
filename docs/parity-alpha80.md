# Fireworks Python SDK alpha.80 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.80`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.80/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.80 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `_snapshot_chain.py` merged-base checkpoints | `training/sdk/snapshot_chain.go` | ported | Added `merged_base` checkpoint normalization and explicit override support. Like base checkpoints, merged-base saves do not emit incremental hotload metadata. |
| `trainer.py` retry-safe trainer creates | `training/sdk/trainer.go` | ported | Trainer creates now always send a client-generated `rlorTrainerJobId` when callers did not provide one, making retries idempotent. HTTP 409 conflicts fetch and return the existing trainer job. |
| `managed.py` generated managed IDs | `training/sdk/managed_lifecycle.go` | ported | Managed provisioning records generated trainer and deployment IDs in the returned handle config and uses generated trainer IDs as requested create IDs. Auto-generated deployment IDs do not trigger an existing-deployment lookup on the first attach. |
| `deployment.py` probe-ready state | `training/sdk/deployment.go` | ported | Deployments that are still `CREATING` in the control plane but pass the inference probe return `READY` in `WaitForReady`. |

## Training SDK

The alpha.79 training matrix remains valid for unchanged modules. Alpha.80 changed snapshot-chain, trainer create, managed provisioning, and deployment readiness behavior; those deltas are covered above and by focused Go tests.

## Broader SDK resources

Generated top-level resources and types are unchanged from alpha.79. The checked-in alpha.80 resource/type report at `docs/resource-type-parity-alpha80.md` has no missing Go resource methods or expected type names.

## Python-only behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, context-aware methods, futures, close methods, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.80 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.80/src` to alpha.79 and found no generated resource/type changes. The training SDK changes are covered by unit tests for checkpoint type normalization, trainer create idempotency/conflict handling, managed generated ID recording, and deployment probe-ready state normalization.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
