# FireTitan Training SDK

This page is a Go-oriented port of the official Python SDK training docs from:

- https://github.com/fw-ai-external/python-sdk/tree/main/src/fireworks/training
- https://github.com/fw-ai-external/python-sdk/tree/main/src/fireworks/training/sdk

The training package mirrors the Python `fireworks.training.sdk` infrastructure
layer. It is intended for algorithm loops and training orchestration, not for
the generated REST resource layer.

## Components

| Go component | Purpose |
| --- | --- |
| `TrainerJobManager` and `TrainerJobConfig` | Create, resume, reconnect, delete, and poll RLOR trainer jobs. Includes training-shape profile resolution. |
| `DeploymentManager` and `DeploymentConfig` | Create, get, delete, scale, reattach, hotload, wait for readiness, and warm up deployments. |
| `DeploymentSampler` | Token-in/token-out completions wrapper for inference deployments, including streaming metrics and routing data. |
| `FiretitanServiceClient` and `FiretitanTrainingClient` | Tinker-style service/training clients with Fireworks-specific checkpoint, sampler, and managed-infra hooks. |
| `FiretitanProvisioningConfig` | Managed trainer/deployment provisioning config consumed by `FiretitanServiceClient`. |
| `WeightSyncer` | Save sampler checkpoints and hotload them into inference deployments with base/delta chain management. |

## Requirements

```sh
export FIREWORKS_API_KEY="fw_..."
```

Training APIs require a training-scoped Fireworks API key. Inference-only keys
can still work for completions and deployment inference, but Fireworks returns
HTTP 401 for trainer lifecycle and training-shape endpoints.

`TrainerJobManager`, `DeploymentManager`, and `FireworksClient` resolve the
account from the API key automatically. `FIREWORKS_ACCOUNT_ID` is still useful
for the generated top-level resource client.

## Provision a trainer and a rollout deployment

Create the trainer first. Set `DeploymentConfig.HotLoadTrainerJob` to the full job name returned by the trainer manager. Use a deployment ID reserved for this run.

```go
package main

import (
	"context"
	"fmt"
	"log"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

func main() {
	ctx := context.Background()
	apiKey := sdk.FireworksAPIKey("")
	baseURL := sdk.FireworksBaseURL("")
	baseModel := "accounts/fireworks/models/qwen3-8b"

	trainers := sdk.NewTrainerJobManager(apiKey, baseURL)
	trainer, err := trainers.CreateAndWait(ctx, sdk.TrainerJobConfig{
		BaseModel:               baseModel,
		DisplayName:             "my-trainer",
		Region:                  "US_VIRGINIA_1",
		AutoSelectTrainingShape: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(trainer.JobName)

	deployments := sdk.NewDeploymentManager(apiKey, baseURL)
	rollout, err := deployments.CreateOrGet(ctx, sdk.DeploymentConfig{
		DeploymentID:      "my-run",
		BaseModel:         baseModel,
		Region:            "US_VIRGINIA_1",
		DeploymentShape:   "accounts/ACCOUNT/deploymentShapes/SHAPE",
		HotLoadTrainerJob: trainer.JobName,
		MinReplicaCount:   1,
		ForTraining:       true,
	}, false)
	if err != nil {
		log.Fatal(err)
	}
	rollout, err = deployments.WaitForReady(ctx, rollout.DeploymentID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(rollout.InferenceModel)
}
```

These resources continue running after the example exits. Managed provisioning can apply a cleanup policy when the service closes.

## Managed Provisioning

`FiretitanServiceClient` can provision and wire a managed trainer/deployment
pair from `FiretitanProvisioningConfig`.

```go
service, err := sdk.NewFiretitanServiceClient(sdk.FiretitanServiceClientOptions{
	APIKey: sdk.FireworksAPIKey(""),
	Config: sdk.FiretitanProvisioningConfig{
		BaseModel:                "accounts/fireworks/models/qwen3-8b",
		TrainingShapeID:          "accounts/ACCOUNT/trainingShapes/TRAINING_SHAPE",
		DeploymentShape:          "accounts/ACCOUNT/deploymentShapes/DEPLOYMENT_SHAPE",
		DisplayName:              "my-managed-run",
		CleanupTrainerOnClose:    true,
		CleanupDeploymentOnClose: string(sdk.CleanupDeploymentOnCloseDelete),
	},
})
if err != nil {
	return err
}

defer service.Close(ctx)
client, err := service.CreateManagedTrainingClient(ctx)
if err != nil {
	return err
}

trainerJobID, err := client.TrainerJobID()
if err != nil {
	return err
}
fmt.Println(trainerJobID)
```

Managed provisioning attaches the deployment as soon as it has the trainer job name. Trainer readiness and deployment setup can overlap. Set `WaitForTrainerBeforeDeployment` to `true` to wait for the trainer before starting deployment setup.

Managed services also support the following operations.

- creating or reconnecting trainer jobs
- creating, reusing, or reattaching deployments
- deployment region inference from deployment-shape accelerator type
- optional separate reference trainer provisioning
- cleanup plans for trainer deletion, deployment deletion, or deployment scale-to-zero
- sampler synchronization state for hotload-backed sampling
- separate 48-hour capacity-placement and post-placement readiness budgets
- preemptible trainers/deployments and bounded inactivity cleanup controls
- `ASYNC` or `SYNC` deployment hotload transitions
- shared trainer capacity through `MaxLoraRank` for multiple LoRA models

## Training Shapes

Managed services select their hardware from `TrainingShapeID` or let Fireworks choose a shape automatically. Configure the shape on the service before creating clients. The managed service fixes the base model and LoRA parameters, except that a shared trainer with `MaxLoraRank` accepts a per-client rank within its capacity. A LoRA policy can reuse its frozen base as the reference model.

### Shape Path

Pass a training shape reference. The backend owns accelerator, image tag, node
count, max context length, and sharding fields. The Go config validates that
you do not set shape-owned infrastructure fields at the same time.

```go
trainer := sdk.NewTrainerJobManager(apiKey, baseURL)

profile, err := trainer.ResolveTrainingProfile(ctx, "accounts/fireworks/trainingShapes/ts-qwen3-8b-policy")
if err != nil {
	return err
}

config := sdk.TrainerJobConfig{
	BaseModel:        "accounts/fireworks/models/qwen3-8b",
	DisplayName:      "shape-run",
	TrainingShapeRef: profile.TrainingShapeVersion,
}

if profile.SupportsLora() {
	fmt.Println("shape supports LoRA launches")
}
```

### Low-level trainer jobs

When calling `TrainerJobManager` directly, a standalone `TrainerJobConfig` also accepts an explicit hardware configuration. This example applies to the low-level job manager.

```go
config := sdk.TrainerJobConfig{
	BaseModel:       "accounts/fireworks/models/qwen3-8b",
	DisplayName:     "manual-run",
	AcceleratorType: "NVIDIA_A100_80GB",
	CustomImageTag:  "0.33.0",
}

accelerators := 8
nodes := 2
config.AcceleratorCount = &accelerators
config.NodeCount = &nodes
```

## Deployment Hotload And Sampling

Deployments can be hotloaded directly:

```go
ok, err := deployMgr.HotloadAndWait(ctx, "my-run", baseModel, "step-1")
if err != nil {
	return err
}
if !ok {
	return fmt.Errorf("hotload did not report success")
}
```

Sampling clients use token IDs:

```go
sampler := sdk.NewFiretitanSamplingClientForDeployment(
	sdk.FireworksBaseURL(""),
	"accounts/my-account/deployments/my-run",
	sdk.FireworksAPIKey(""),
)

maxTokens := 16
result, err := sampler.Sample(ctx, []int{1, 2, 3}, 1, sdk.FiretitanSamplingParams{
	MaxTokens: &maxTokens,
})
if err != nil {
	return err
}
fmt.Println(result.Sequences)
```

`FiretitanTrainingClient.SaveWeightsForSampler` and
`SaveWeightsAndHotload` provide Python-compatible sampler checkpoint behavior.
For full-parameter training, the checkpoint chain defaults to a base checkpoint
followed by delta checkpoints; LoRA checkpoints remain base checkpoints.

Sampling uses behavior-policy `sampling_logprob` values for generated tokens,
keeps one logical `X-Request-Id` across retries, honors `Retry-After`, and
returns `SamplingRequestError` with payload-free request identity and status
metadata.

For serverless training sessions, `CreateSamplingClient` routes checkpoint
paths through `/training/v1/serverless` and adds `X-Session-Affinity`.
`CreateServerlessBaseSamplingClient` selects the session base-model route.

Bind replay capture to the policy client with `service.CreateSamplingClientForTraining(ctx, modelPath, tokenizer, controller, policy)`. The trainer advertises the shared Parquet store. Capture requests validate the store and align references with the returned token IDs. Use fresh rollouts when migrating inline routing matrices.

| Sampling parameter | Returned sequence field | Use |
| --- | --- | --- |
| `IncludeRoutingMatrix: true` | `RoutingReferences` | Router replay |
| `SamplingMask: true` | `SamplingMaskReferences` | Replay the sampling support |
| `SampleExtraTokens: &draws` | `SampleExtraReferences` | Draw 1–64 extra tokens per position with stochastic decoding |
| `TopSamplingLogprobs: &k` | `TopSamplingReferences` | Capture the top candidates per position |

Choose one of extra sampling and top sampling for each request. For REINFORCE score centering, call `policy.ValidateLossCapabilities("reinforce", lossConfig)` before collecting rollouts, then include references matching the chosen estimator in each datum. `ModelInputFromIntsWithExtraSampling` and `ModelInputFromIntsWithTopSampling` validate the reference lengths. An adaptive controller manages sampling concurrency by default. Pass `sdk.WithBackendManagedConcurrency()` when constructing a `DeploymentSampler` to delegate scheduling to the backend.

Hotload uses `ResetPromptCacheNewSession` by default. `ResetPromptCacheAll` and `ResetPromptCacheNone` select the other server policies. RDMA publication requires compatible trainer and rollout replicas, including LoRA-specific support for a LoRA policy. Once RDMA is required, the SDK surfaces failures directly. For a `merged_base` checkpoint, `SaveWeightsForSamplerOptions.ExportRounding` accepts `nearest` or `stochastic`.

## Cross-Job Checkpoint References

Use cross-job checkpoint references when resuming or loading checkpoints from a
different trainer job.

```go
ref, err := sdk.MakeCrossJobCheckpointRef("source-trainer-job", "checkpoint-name")
if err != nil {
	return err
}

resolved, err := sdk.ResolveCheckpointPath("checkpoint-name", "source-trainer-job")
if err != nil {
	return err
}
fmt.Println(ref, resolved)
```

For checkpoint promotion, pass the full checkpoint resource name returned by Fireworks together with the output model ID and base model. For example, a dedicated trainer checkpoint uses `accounts/ACCOUNT/rlorTrainerJobs/JOB/checkpoints/CHECKPOINT`. For `LoadAdapter`, pass the PEFT model resource name `accounts/ACCOUNT/models/MODEL`.

## Forward/Backward And Metrics

The Go port exposes Go-native equivalents for Tinker-style training methods and
built-in loss names. `LossFnDAPO`, `LossFnGSPO`, and normalization helpers cover
the Python SDK's built-in loss patch behavior without Python monkeypatching.

For `cross_entropy`, the training layer preserves the Python behavior of
tracking response-token accounting so callers can compute mean loss from
summed loss metrics.

Accumulate gradients by making multiple `ForwardBackward` calls before `OptimStep`. On customer-managed RLOR jobs, `service.LogTrainingMetrics(ctx, record)` sends a numeric `step`, finite numeric metric values and an optional string `session_id`. It submits each batch once. If the response is lost, check the job before resubmitting.

## Error Handling And Retries

The training REST client includes:

- structured API error body parsing
- retry helpers for retryable HTTP and network errors
- status hints for common failure modes
- longer polling paths for deployment hotload and readiness checks

HTTP 425 during hotload is handled at hotload call sites because it means the
deployment is still loading, not a generic request retry.

## Setup CLI

Use the setup helpers to create a standalone trainer or a standalone inference deployment. Each command prints JSON and saves the same information to the specified file. To connect training and rollout, use one of the provisioning examples above.

```sh
go run ./cmd/fireworks-training setup-trainer \
  --display-name my-trainer \
  --base-model accounts/fireworks/models/qwen3-8b \
  --output-file trainer_info.json

go run ./cmd/fireworks-training setup-deployment \
  --deployment-id my-inference-deployment \
  --base-model accounts/fireworks/models/qwen3-8b \
  --deployment-shape accounts/ACCOUNT/deploymentShapes/SHAPE \
  --output-file deployment_info.json
```

## Live Contract Tests

```sh
FIREWORKS_SDK_GO_LIVE=1 \
FIREWORKS_API_KEY=fw_... \
go test ./training/sdk -run Live -count=1
```

With only `FIREWORKS_API_KEY`, the live selector runs a read-only account-list
smoke test. Trainer/deployment/checkpoint tests require additional environment
variables documented in [`live-contract-tests.md`](live-contract-tests.md).

## Upstream Docs Ported

This page ports the official Python training docs into Go form. Python-only
behaviors are represented explicitly rather than copied literally:

- Python monkeypatching is replaced with Go constructors and helper methods.
- Python async/future behavior is represented by context-aware methods and
  `Future` wrappers.
- Pydantic model mutation is not needed; Go uses typed structs and maps.

## Native training protocol and reserved sessions

`NewNativeTrainingRuntime(restClient, sessionID)` implements the JSON training protocol. Pass it as `CreateFiretitanTrainingClientOptions.ComputeBackend` to create a model on an existing trainer endpoint. The model client then exposes state, adapter and weight-sync operations. `training/types` contains generated wire models and the upstream v2 protobuf schema. Caller contexts bound future polling and result downloads.

For a reserved serverless session, persist a unique `ts-...` session ID and ownership marker before calling `api.ReserveTrainingSession(ctx, id, marker)`. Use `api.GetTrainingSession(ctx, fullName)` to reconcile an interrupted reservation. The full name is `accounts/ACCOUNT/trainingSessions/SESSION`. A missing session returns `nil, nil`.

```go
service, err := api.OpenReservedTrainingSession(ctx, fullName, marker)
if err != nil {
	return err
}
defer service.Close(ctx)

rank := 8
policy, err := service.CreateTrainingClient(ctx, sdk.CreateFiretitanTrainingClientOptions{
	BaseModel: baseModel,
	LoraRank:  &rank,
})
if err != nil {
	return err
}
fmt.Println(policy.ModelID)
```

Open the fresh session from one owner. The service verifies the marker and unbound state, sends heartbeats and shares model sequence counters across clients. Closing it ends the heartbeats. Restore sampler handles with `FiretitanSamplingClientFromHandle` and the receiving process's key.

## Training tokenizers

Set `FiretitanProvisioningConfig.TokenizerModel` and call `policy.GetTokenizer(ctx, sdk.TokenizerOptions{})` to load the trainer's tokenizer. `LoadFastTokenizer` also opens a local saved `tokenizer.json` directory or a Hugging Face repository directly. For chat templates that iterate over tool fields, `RenderChatJSON` preserves the key order in caller-supplied JSON. Call `Close()` on a `*sdk.FastTokenizer` when the deployment finishes using it.

The Go module embeds the Hugging Face tokenizer and MiniJinja WebAssembly runtime. Ordinary Go builds use that artifact directly. To rebuild it from the repository root with Rust 1.94.0 and Zsh, use

```sh
rustup toolchain install 1.94.0 --profile minimal --target wasm32-wasip1
rustup run 1.94.0 zsh training/sdk/internal/hftokenizer/build.zsh
```

For lifecycle, checkpoint, replay and TITO changes, see the [1.2.39 release matrix](parity-1.2.39.md).
