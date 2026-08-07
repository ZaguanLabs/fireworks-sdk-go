# Fireworks Python SDK 1.2.6 parity matrix

Baseline: `fireworks_ai` `1.2.6` from
`docs/fireworks-py/python-sdk-1.2.6/src/fireworks`.

## Training SDK delta

| Python area | Go area | Status |
| --- | --- | --- |
| Structured Training API errors, promotion formatting, `Retry-After` | `training/sdk/errors.go`, `fireworks_client.go`, `trainer.go` | ported |
| Validated client-source/session headers and transport pooling | `training/sdk/rest_client.go` | ported |
| MoE architecture lookup | `training/sdk/fireworks_client.go` | ported |
| Pending-capacity timeout, tombstones, direct route fallback, inactivity and preemptible controls | `training/sdk/trainer.go`, `managed_lifecycle.go` | ported |
| Hotload transition mode, CMEK source/header, preemptible deployment, replica identity | `training/sdk/deployment.go`, `managed.go` | ported |
| Multi-model `max_lora_rank`, reference rules/filtering, endpoint context resolution | `training/sdk/managed.go`, `managed_lifecycle.go`, `service_client.go` | ported |
| Behavior-policy logprobs, routing matrices, stable request IDs, structured errors, retry ownership | `training/sdk/sampling.go` | ported |
| Adaptive concurrency interval adjustment and default controller | `training/sdk/sampling.go` | ported |
| Serverless checkpoint/base sampling and session affinity | `training/sdk/service_client.go` | ported |
| Public sampler path preservation and per-snapshot LoRA metadata | `training/sdk/service_client.go`, `managed.go` | ported |
| R3 diagnostics, request-size accounting, parallel send controls | `training/sdk/forward_backward.go`, `client_helpers.go` | go-native |

## Go-native equivalents

Python 1.2.6 patches Tinker/Pydantic classes and future body timeouts at runtime.
Go uses explicit request structs, JSON encoding, `context.Context`, typed
futures, and exported validation helpers, so those monkeypatches have no
runtime counterpart. The observable wire behavior is represented directly.

Top-level generated REST resources and type exports are checked by
`docs/resource-type-parity-1.2.6.md`; no Python resource methods or expected
type names are missing.
