# Fireworks Python SDK alpha.84 parity matrix

Baseline: `fireworks_ai` `1.2.0-alpha.84`.

This matrix tracks the Go port against `docs/fireworks-py/python-sdk-1.2.0-alpha.84/src/fireworks/training/sdk`.

Status values:

- `ported`: equivalent Go behavior exists and has focused tests.
- `go-native`: Python behavior is not directly portable; Go should expose an idiomatic equivalent.

## Alpha.84 Delta

| Python area | Go area | Status | Notes |
| --- | --- | --- | --- |
| `GradNormMetricsMode` and `emit_grad_norm_metrics` | `training/sdk/client_helpers.go`, `training/sdk/service_client.go`, `training/sdk/future_facade.go` | ported | Go exposes `GradNormMetricsMode`, accepts bool/string modes through `OptimStepExt`, validates invalid modes before dispatch, and forwards the normalized value to compute backends. |
| Serverless session/run identity helpers | `training/sdk/client_helpers.go`, `training/sdk/service_client.go` | ported | Service clients expose `TrainingSessionID`, `TrainingSessionName`, and `ServerlessRunName`; training clients expose `RunID` and `RunName` for run-scoped model IDs. |
| Inference deployment sampler creation and cleanup | `training/sdk/service_client.go` | ported | Managed services can create/reuse inference deployments for sampling, inherit managed region/shape defaults, reject terminal deployments, track cleanup before readiness, and continue close cleanup through all owned resources. |
| `DeploymentConfig.from_training_profile` shape guardrail | `training/sdk/deployment.go` | ported | Go exposes `DeploymentConfigFromTrainingProfile` plus `ExpectedDeploymentShape` validation before deployment creation. |
| Managed reference auto shape selection | `training/sdk/managed.go`, `training/sdk/managed_lifecycle.go`, `training/sdk/trainer.go` | ported | Full-parameter references without an explicit reference shape now create a frozen reference trainer through backend auto-selection; legacy `FORWARD_ONLY` reference shapes are rejected in favor of `LORA_TRAINER`. |
| Trainer auto shape path | `training/sdk/trainer.go` | ported | `TrainerJobConfig.AutoSelectTrainingShape` omits manual infra, sends selector fields like `max_context_length`, `region`, and `custom_image_tag`, and suppresses manual `skipValidations=true`. |
| Python Pydantic AdamParams patch | Go-native explicit fields | go-native | Python mutates Tinker Pydantic models at runtime; Go has explicit option fields and backend request structs. |

## Training SDK

The alpha.83 training matrix remains valid for unchanged modules. Alpha.84 changed grad-norm metrics controls, serverless identity surfacing, inference deployment sampling helpers, deployment shape guardrails, reference trainer shape selection, and trainer auto-shape launch semantics. Those deltas are covered by focused Go tests.

## Broader SDK Resources

Generated top-level resources and type definitions are unchanged from alpha.83. The checked-in alpha.84 resource/type report at `docs/resource-type-parity-alpha84.md` has no missing Go resource methods or expected type names.

## Python-Only Behavior

These remain Go-native rather than literal ports:

- Monkeypatching `tinker.ServiceClient`.
- Pydantic model field mutation, rebuild hooks, and cached internal serializer refresh.
- Python coroutine/async signature checks.
- Python context-manager restoration semantics for patched classes.

The Go port exposes explicit constructors, typed options, context-aware methods, futures, close methods, direct JSON payloads, and clear unsupported-operation errors instead.

## Final Sweep Notes

The alpha.84 sweep compared `docs/fireworks-py/python-sdk-1.2.0-alpha.84/src` to alpha.83. Resource methods and generated types remained covered. The training deltas are represented in Go with explicit APIs and tests for grad norm metrics, deployment guardrails, auto shape launch behavior, serverless identity helpers, and inference deployment cleanup.

Remaining validation work is live-only: run the opt-in Fireworks contract tests for trainer/deployment/provisioning workflows when suitable real resource IDs are available.
