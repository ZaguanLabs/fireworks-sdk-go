# Fireworks Python SDK alpha.78 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.78`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.78/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.78 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `_constants.py` cleanup exports | `training/sdk/constants.go`, `managed_cleanup.go` | ported | Added `DeploymentCleanupOnClose`, delete/scale-to-zero cleanup constants, and SDK-managed rollout deployment annotation constants while preserving older Go cleanup aliases. |
| `managed.py` trainer ownership | `training/sdk/managed_lifecycle.go` | ported | Stable trainer IDs now track whether the trainer was created by this run. Reused/reconnected trainers are not deleted on close even when cleanup is requested; newly created stable trainers still are. |
| `managed.py` deployment ownership | `training/sdk/managed_deployment.go`, `managed_lifecycle.go` | ported | Deployment attachment plans now track created vs reused/reattached deployments. Close cleanup only applies to deployments this run created. |
| `managed.py` deployment annotations | `training/sdk/managed_deployment.go` | ported | SDK-created managed rollout deployments include `fireworks-training-sdk/managed-rollout: true`. |
| `managed.py` reference cleanup policy | `training/sdk/managed.go`, `managed_lifecycle.go` | ported | Reference configs carry the requested cleanup policy; lifecycle ownership tracking prevents deleting reused reference trainers. |

## Training SDK

The alpha.77 training matrix remains valid for unchanged modules. Alpha.78 only changed `_constants.py` and `managed.py`; those deltas are covered above and by focused Go tests.

## Broader SDK resources

Generated top-level resources and types are unchanged from alpha.77. The checked-in alpha.78 resource/type report at `docs/resource-type-parity-alpha78.md` has no missing Go resource methods or expected type names.

## Python-only behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, context-aware methods, futures, close methods, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.78 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.78/src` to alpha.77 and found no generated resource/type changes. The changed managed lifecycle behavior is covered by unit tests for stable trainer ownership, deployment ownership, rollout annotations, and reference cleanup policy.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
