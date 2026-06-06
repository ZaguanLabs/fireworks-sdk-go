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
| `concurrency.py` | `training/sdk/sampling.go` | ported | Fixed and adaptive AIMD concurrency controllers, step-level metrics, cache hit summaries, sampler integration, and max-concurrency fallback construction are covered. |
| `fireworks_client.py` | `training/sdk/fireworks_client.go` | ported | Checkpoint promotion/listing and long-running operation polling are covered. |
| `training_spec.py` | `training/sdk/training_spec.go` | ported | Training spec encoding and scheduler/warmup calculations are covered. |
| `weight_syncer.py` | `training/sdk/weight_syncer.go`, `future_facade.go` | ported | Save/hotload lifecycle, hotload manager readiness, timing, sampler client helpers, and training-client facade wrappers are covered. |
| `managed.py` | `managed*.go`, `service*.go`, `tokenizer.go`, `base_only.go` | partial | Config normalization, trainer/deployment provisioning orchestration, reattach, sampler/weight-sync attachment, separate reference provisioning, reference job IDs/release, optional and required service metadata accessors, metadata resolution, cleanup planning, tokenizer/base-only helpers, lazy REST responses, public service/training-client facades, and opt-in live contract tests exist. Needs live execution against Fireworks. |
| `client.py` low-level helpers | `client_helpers.go`, `resume_helpers.go`, `training_registry.go`, `forward_backward.go`, `embedding.go`, `service_client.go`, `future.go`, `future_facade.go` | partial | Session IDs, checkpoint refs, managed config aliases, duplicate registry, response-token accounting, embedding pooling, built-in loss names, R3 model input helpers, checkpoint delegation, base/reference client facades and futures, service-level resume-from-state and futures, state load/save facade hooks and futures, adapter loading facade hooks and futures, managed lifecycle wiring, service-level sampler/deployment-sampler facades, sampler backend hooks, tokenizer model resolution, and Go-native futures are covered. |
| `sampling.py` | `sampling.go`, `service_client.go`, `future_facade.go` | ported | The FireTitan sampler, output shape helpers, concurrent `n` sampling, metrics, hotload/truncation retries, jittered transient retry backoff, facade sampler hooks, service-level direct deployment sampler access, close semantics, and Go-native future wrappers are covered. Python async/thread-loop and context-manager mechanics are represented by context-aware methods, futures, and explicit `Close()`. |
| `tinker_compat.py` | `service_init.go`, `service_helpers.go` | go-native | Python monkeypatch/context-manager behavior is not portable. Go should expose explicit construction helpers instead. |
| `patches/_builtin_loss_fn_patch.py` | `forward_backward.go` | go-native | Python mutates Pydantic literals; Go exposes `LossFnDAPO`, `LossFnGSPO`, and normalization helpers. |
| `patches/_discriminator_patch.py` | n/a | go-native | Pydantic serialization patch is Python-only. Go uses maps/typed request helpers and does not need discriminator patching. |
| `patches/_model_utils.py` | n/a | go-native | Python-only Pydantic rebuild helper used by monkeypatches; Go has no generated Pydantic model graph to rebuild. |
| `patches/_tinker_r3_patch.py` | `forward_backward.go` | go-native | Go exposes `ModelInputFromInts` and routing matrix helpers. |
| `scripts/setup_trainer.py` | `training/sdk/setup_cli.go`, `cmd/fireworks-training` | ported | Go helper and CLI subcommand mirror trainer creation, polling, defaults, env fallback, and JSON output. |
| `scripts/setup_deployment.py` | `training/sdk/setup_cli.go`, `cmd/fireworks-training` | ported | Go helper and CLI subcommand mirror deployment creation, readiness polling, defaults, env fallback, and JSON output. |

## Broader SDK resources

| Area | Status | Notes |
| --- | --- | --- |
| Generated top-level resources/types | ported | Go has resource clients, typed aliases, generated types, version parity, smoke tests, and a checked-in alpha.76 resource/type report at `docs/resource-type-parity-alpha76.md`. |
| Streaming/text/image/file primitives | partial | Core HTTP, file, stream, and typed resource helpers exist. Stream SSE framing now mirrors Python for comments, multi-line data, ping filtering, `[DONE]` prefixes, `message_stop`, and terminal response close. Needs endpoint-by-endpoint parity audit outside `training/sdk`. |

## Python-only behavior

These should not be ported literally:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation and rebuild hooks.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port should instead provide explicit constructors, context-aware methods, and clear unsupported-operation errors.

## Priority backlog

1. Run the opt-in live contract tests against Fireworks and record results.
