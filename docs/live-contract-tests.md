# Live Fireworks Contract Tests

The Go test suite includes opt-in live tests for the FireTitan training paths.
They are skipped by default and only run when `FIREWORKS_SDK_GO_LIVE=1`.

Run a focused live pass with:

```sh
FIREWORKS_SDK_GO_LIVE=1 \
FIREWORKS_API_KEY=fw_... \
go test ./training/sdk -run Live -count=1
```

With only `FIREWORKS_API_KEY` set, the suite runs a read-only account-list
smoke test and skips the resource-specific tests below.

## Account List Smoke

Required:

- `FIREWORKS_API_KEY`

This calls `GET /v1/accounts?pageSize=1` and verifies that the SDK can
authenticate and parse a non-empty account list.

## Trainer Reconnect And Checkpoints

Required:

- `FIREWORKS_LIVE_TRAINER_JOB_ID`

Optional:

- `FIREWORKS_LIVE_RECONNECT=1` to exercise `ReconnectAndWait`; otherwise the
  test waits for an existing running trainer.
- `FIREWORKS_LIVE_TRAINER_TIMEOUT_S`

## Checkpoint Promotion

Promotion is destructive/costly enough to require a second opt-in:

- `FIREWORKS_LIVE_PROMOTE=1`
- `FIREWORKS_LIVE_OUTPUT_MODEL_ID`
- `FIREWORKS_LIVE_BASE_MODEL`

Identify the checkpoint with either:

- `FIREWORKS_LIVE_CHECKPOINT_NAME`, or
- `FIREWORKS_LIVE_TRAINER_JOB_ID` and `FIREWORKS_LIVE_CHECKPOINT_ID`

Optional:

- `FIREWORKS_LIVE_DEPLOYMENT_ID`
- `FIREWORKS_LIVE_PROMOTE_TIMEOUT_S`

## Deployment Hotload And Sampling

Required:

- `FIREWORKS_LIVE_DEPLOYMENT_ID`
- `FIREWORKS_LIVE_BASE_MODEL`
- `FIREWORKS_LIVE_SNAPSHOT_IDENTITY`

Optional:

- `FIREWORKS_LIVE_HOTLOAD_API_URL`
- `FIREWORKS_LIVE_INFERENCE_URL`
- `FIREWORKS_LIVE_HOTLOAD_TIMEOUT_S`
- `FIREWORKS_LIVE_SAMPLE=1` to sample after hotload
- `FIREWORKS_LIVE_PROMPT_TOKENS=1,2,3`

## Managed Provisioning

Managed resource creation is also opt-in:

- `FIREWORKS_LIVE_MANAGED_CREATE=1`
- `FIREWORKS_LIVE_BASE_MODEL`

Optional:

- `FIREWORKS_LIVE_TRAINING_SHAPE_ID`
- `FIREWORKS_LIVE_DEPLOYMENT_SHAPE`
- `FIREWORKS_LIVE_DEPLOYMENT_ID`
- `FIREWORKS_LIVE_CREATE_DEPLOYMENT=0`
- `FIREWORKS_LIVE_CLEANUP_TRAINER=1`
- `FIREWORKS_LIVE_CLEANUP_DEPLOYMENT=scale_to_zero` or `delete`
- `FIREWORKS_LIVE_DISPLAY_NAME`
- `FIREWORKS_LIVE_SKIP_VALIDATIONS=1`
- `FIREWORKS_LIVE_DISABLE_SPECULATIVE_DECODING=1`
