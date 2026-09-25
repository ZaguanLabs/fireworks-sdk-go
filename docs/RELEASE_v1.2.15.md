# Fireworks SDK Go v1.2.15

Aligns the Go SDK with the official Fireworks Python SDK 1.2.15.

- Adds deployment shape matching, explicit shapeless-deployment risk acceptance, and sampler checkpoint save intervals; removes the upstream-deleted dataset creation filter.
- Adds paginated SFT/DPO checkpoint listing and asynchronous checkpoint promotion.
- Preserves structured trainer failure status and waits for control-plane readiness before and after reattachment.
- Uses parent training shapes unless a concrete version is explicitly pinned. Managed trainer/deployment provisioning overlaps by default; `WaitForTrainerBeforeDeployment` selects sequential startup.
- Negotiates model-scoped comms v2 and compact Parquet routing references, with shared inference capability probes, storage validation, TTL headers, cancellation isolation, and legacy inference fallback.
- Preserves partial prompt routing and top-k logprobs in sampling and TITO artifacts. Normalizes empty assistant refusal metadata on admission and streams artifact JSON into compression.
- Fixes a TITO sidecar startup/shutdown race.

## Compatibility

Code using the removed `Filter` field in dataset creation or transformed-dataset parameters must remove it. Dataset list filtering is unaffected.

Existing training backend adapters remain supported. Adapters can implement `TrainingModelCreator` (or supply `ModelCreationResponse`) to retain trainer capabilities, and use `ForwardBackwardOptions.RequestBody` to include the negotiated comms field. Legacy responses retain comms v1 and inline routing.

## Validation

`go test -race ./...`, `go build ./...`, and `go vet ./...` pass. The generated resource/type catalog reports no missing names. Tests cover HTTP contracts, routing negotiation and validation, partial prompt echoes, artifact round trips, provisioning order, cancellation, and checkpoint promotion. Live paid API calls were not run.

See [the parity matrix](parity-1.2.15.md) for scope and runtime differences.
