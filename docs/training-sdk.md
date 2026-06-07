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

## Minimal Lifecycle

```go
package main

import (
	"context"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

func main() {
	ctx := context.Background()
	apiKey := sdk.FireworksAPIKey("")
	baseURL := sdk.FireworksBaseURL("")
	baseModel := "accounts/fireworks/models/qwen3-8b"

	deployMgr := sdk.NewDeploymentManager(apiKey, baseURL)
	deployment, err := deployMgr.CreateOrGet(ctx, sdk.DeploymentConfig{
		DeploymentID: "my-run",
		BaseModel:    baseModel,
		Region:       "US_VIRGINIA_1",
	}, false)
	if err != nil {
		panic(err)
	}

	deployment, err = deployMgr.WaitForReady(ctx, deployment.DeploymentID)
	if err != nil {
		panic(err)
	}

	trainerMgr := sdk.NewTrainerJobManager(apiKey, baseURL)
	endpoint, err := trainerMgr.CreateAndWait(ctx, sdk.TrainerJobConfig{
		BaseModel:           baseModel,
		DisplayName:         "my-trainer",
		HotLoadDeploymentID: deployment.DeploymentID,
		Region:              "US_VIRGINIA_1",
	})
	if err != nil {
		panic(err)
	}

	service, err := sdk.NewFiretitanServiceClient(sdk.FiretitanServiceClientOptions{
		APIKey:  "tml-local",
		BaseURL: endpoint.BaseURL,
		Config: sdk.FiretitanProvisioningConfig{
			BaseModel: baseModel,
		},
	})
	if err != nil {
		panic(err)
	}

	policy, err := service.CreateTrainingClient(ctx)
	if err != nil {
		panic(err)
	}

	syncer := sdk.NewWeightSyncer(sdk.WeightSyncerConfig{
		PolicyClient: policy,
		DeployMgr:    deployMgr,
		DeploymentID: deployment.DeploymentID,
		BaseModel:    baseModel,
	})

	if _, err := syncer.SaveAndHotload(ctx, "step-1"); err != nil {
		panic(err)
	}
}
```

## Managed Provisioning

`FiretitanServiceClient` can provision and wire a managed trainer/deployment
pair from `FiretitanProvisioningConfig`.

```go
service, err := sdk.NewFiretitanServiceClient(sdk.FiretitanServiceClientOptions{
	APIKey: sdk.FireworksAPIKey(""),
	Config: sdk.FiretitanProvisioningConfig{
		BaseModel:       "accounts/fireworks/models/qwen3-8b",
		TrainingShapeID: "accounts/fireworks/trainingShapes/ts-qwen3-8b-policy",
		DeploymentShape: "accounts/fireworks/deploymentShapes/my-shape",
		DisplayName:     "my-managed-run",
	},
})
if err != nil {
	return err
}

client, err := service.CreateManagedTrainingClient(ctx)
if err != nil {
	return err
}
defer service.Close(ctx)

trainerJobID, err := client.TrainerJobID()
if err != nil {
	return err
}
fmt.Println(trainerJobID)
```

Managed provisioning supports:

- creating or reconnecting trainer jobs
- creating, reusing, or reattaching deployments
- deployment region inference from deployment-shape accelerator type
- optional separate reference trainer provisioning
- cleanup plans for trainer deletion, deployment deletion, or deployment scale-to-zero
- sampler synchronization state for hotload-backed sampling

## Training Shapes

Two launch paths mirror the Python SDK.

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

### Manual Path

Omit `TrainingShapeRef` and set the infrastructure fields directly.

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

## Cross-Job Checkpoint References

Use cross-job checkpoint references when resuming or loading checkpoints from a
different trainer job.

```go
ref, err := sdk.MakeCrossJobCheckpointRef("source-trainer-job", "checkpoint-name")
if err != nil {
	return err
}

resolved, err := sdk.ResolveCheckpointPath("checkpoint-name", "source-trainer-job")
```

## Forward/Backward And Metrics

The Go port exposes Go-native equivalents for Tinker-style training methods and
built-in loss names. `LossFnDAPO`, `LossFnGSPO`, and normalization helpers cover
the Python SDK's built-in loss patch behavior without Python monkeypatching.

For `cross_entropy`, the training layer preserves the Python behavior of
tracking response-token accounting so callers can compute mean loss from
summed loss metrics.

## Error Handling And Retries

The training REST client includes:

- structured API error body parsing
- retry helpers for retryable HTTP and network errors
- status hints for common failure modes
- longer polling paths for deployment hotload and readiness checks

HTTP 425 during hotload is handled at hotload call sites because it means the
deployment is still loading, not a generic request retry.

## Setup CLI

The Go CLI mirrors the Python SDK setup scripts:

```sh
go run ./cmd/fireworks-training setup-trainer \
  --display-name my-trainer \
  --training-shape-ref accounts/ACCOUNT/trainingShapes/SHAPE

go run ./cmd/fireworks-training setup-deployment \
  --deployment-id my-deployment \
  --deployment-shape accounts/ACCOUNT/deploymentShapes/SHAPE
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
