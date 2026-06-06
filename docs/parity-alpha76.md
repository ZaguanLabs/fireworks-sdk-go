# Fireworks Python SDK alpha.76 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.76`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.76/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `partial`: important behavior exists, but public orchestration or some edge cases remain.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.
- `missing`: not implemented yet.

## Training SDK

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `_constants.py` | `training/sdk/constants.go` | ported | Timeouts, polling intervals, and defaults are covered. |
| `_rest_client.py` | `training/sdk/rest_client.go` | ported | Account discovery, headers, retry/backoff, and request timeouts are covered. |
| `_snapshot_chain.py` | `training/sdk/snapshot_chain.go` | ported | Base/delta checkpoint type resolution and incremental metadata are covered. |
| `_sse.py` | `training/sdk/sse.go` | ported | SSE framing, EOF handling, and streaming completion assembly are covered. |
| `errors.py` | `training/sdk/errors.go` | ported | Structured SDK errors, API body parsing, and retryable request helpers are covered. |
| `trainer.py` | `training/sdk/trainer.go` | ported | Create, wait, reconnect/resume, adapter loading, training profile parsing, checkpoint listing, inactivity timeout, and validation warnings are covered. |
| `deployment.py` | `training/sdk/deployment.go`, `sampling.go` | ported | Create/get/delete/scale, hotload, reattach, warmup, token sampling, routing matrices, concurrency, SSE truncation retries, and server metrics are covered. |
| `fireworks_client.py` | `training/sdk/fireworks_client.go` | ported | Checkpoint promotion/listing and long-running operation polling are covered. |
| `training_spec.py` | `training/sdk/training_spec.go` | ported | Training spec encoding and scheduler/warmup calculations are covered. |
| `weight_syncer.py` | `training/sdk/weight_syncer.go`, `future_facade.go` | ported | Save/hotload lifecycle, hotload manager readiness, timing, sampler client helpers, and training-client facade wrappers are covered. |
| `managed.py` | `managed*.go`, `service*.go`, `tokenizer.go`, `base_only.go` | partial | Config normalization, trainer/deployment provisioning orchestration, reattach, sampler/weight-sync attachment, separate reference provisioning, metadata resolution, cleanup planning, tokenizer/base-only helpers, lazy REST responses, and public service/training-client facades exist. Needs live contract coverage. |
| `client.py` low-level helpers | `client_helpers.go`, `resume_helpers.go`, `training_registry.go`, `forward_backward.go`, `embedding.go`, `service_client.go`, `future.go`, `future_facade.go` | partial | Session IDs, checkpoint refs, managed config aliases, duplicate registry, response-token accounting, embedding pooling, built-in loss names, R3 model input helpers, checkpoint delegation, managed lifecycle wiring, sampler backend hooks, tokenizer model resolution, and Go-native futures are covered. |
| `sampling.py` | `sampling.go`, `service_client.go`, `future_facade.go` | partial | The FireTitan sampler, output shape helpers, facade sampler hooks, and future wrappers are covered. |
| `tinker_compat.py` | `service_init.go`, `service_helpers.go` | go-native | Python monkeypatch/context-manager behavior is not portable. Go should expose explicit construction helpers instead. |
| `patches/_builtin_loss_fn_patch.py` | `forward_backward.go` | go-native | Python mutates Pydantic literals; Go exposes `LossFnDAPO`, `LossFnGSPO`, and normalization helpers. |
| `patches/_discriminator_patch.py` | n/a | go-native | Pydantic serialization patch is Python-only. Go uses maps/typed request helpers and does not need discriminator patching. |
| `patches/_tinker_r3_patch.py` | `forward_backward.go` | go-native | Go exposes `ModelInputFromInts` and routing matrix helpers. |
| `scripts/setup_trainer.py` | none | missing | CLI equivalent has not been added. |
| `scripts/setup_deployment.py` | none | missing | CLI equivalent has not been added. |

## Broader SDK resources

| Area | Status | Notes |
| --- | --- | --- |
| Generated top-level resources/types | partial | Go has resource clients, typed aliases, generated types, version parity, and smoke tests. Needs a generated resource/type diff against the alpha.76 OpenAPI-derived Python package. |
| Streaming/text/image/file primitives | partial | Core HTTP, file, stream, and typed resource helpers exist. Needs endpoint-by-endpoint parity audit outside `training/sdk`. |

## Python-only behavior

These should not be ported literally:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port should instead provide explicit constructors, context-aware methods, and clear unsupported-operation errors.

## Priority backlog

1. Add CLI equivalents for setup trainer/deployment scripts.
2. Generate a broader SDK resource/type parity report for non-training endpoints.
3. Add live/contract tests for trainer, deployment, save/hotload/sample, checkpoint promote, and reconnect flows.
