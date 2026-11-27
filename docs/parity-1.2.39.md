# Fireworks Go SDK 1.2.39

This release targets the official Fireworks Python SDK 1.2.39. The PyPI source archive has SHA-256 `921130bd6ab48de1520f4826d441a012b590c3d4f0e88dd06acd443633fbeb45`. The upstream distribution was built with `python -m build` and its generated source archive was unpacked under `docs/fireworks-py/`.

## REST client

`client.WatermarkDetections.CreateTyped` accepts text and returns the watermark object and detection result. Short input can return `not_detected`. Completion requests support token-ID-only prompt scoring, including null values in token log probabilities. The generated management types include creator metadata and held-out DPO evaluation options. The typed model-upload validator follows the updated upstream signature.

The [REST resource and type comparison](resource-type-parity-1.2.39.md) covers the resource methods and all 359 expected public type names. The catalog checks names. HTTP contract tests check request paths and wire formats.

## Training and sampling

- Provision the trainer before its rollout deployment. Use a validated training shape and configure the base model and LoRA rank on the managed service. Promote checkpoints by their full resource name, and load adapters using an authorized PEFT model name.
- Use `ReserveTrainingSession` and `OpenReservedTrainingSession` to persist creation intent and resume an unbound session after checking ownership.
- Bind the rollout sampler to its trainer to capture Router Replay matrices, top-k/top-p masks, probability heads and independent extra tokens in the trainer's shared store. Recapture older inline replay data before training.
- Dedicated LoRA weight synchronization over RDMA checks both the requested publication mode and the trainer/deployment capabilities. Hotload accepts explicit prompt-cache policies. Fixed and backend-managed sampler concurrency are available.
- TITO records tree lineage so interleaved rollouts can resume individual branches. The released Python 1.2.39 source writes schema 2. Earlier Go schema-1 archives can still be read. New artifacts support streamed integrity checks. Normally finished calls with invalid tool arguments are eligible for scoring only when the renderer confirms closure and all sampled token log probabilities are finite.
- MLflow ingest supports required/optional configuration, redacted run metadata, batched metrics and write-once parameters. Go callers close a run with `Finish` and pass its terminal status. Trainer job metrics use the Fireworks control plane independently.

## Tokenizers

Kimi K2-family tokenizers and the K3/K3.1 XTML encoders load reviewed data using native Go readers. For existing ordered K2 Base tool schemas, pass `KimiChatOptions.ToolsJSON`. The [K3](../training/sdk/licenses/Kimi-K3.txt) and [K3.1](../training/sdk/licenses/Kimi-K3.1-Pro.txt) encoders carry Moonshot AI's notices.

`LoadFastTokenizer` opens a Hugging Face `tokenizer.json` locally or from a Hub snapshot. It runs Hugging Face tokenizers and MiniJinja in an embedded WASI module through wazero. Applications build with Go alone. The guest receives tokenizer data, templates and explicit inputs, with no host filesystem mounts or checkpoint Python execution. Use `RenderChatJSON` to retain JSON member order in existing tool declarations. Close a fast tokenizer after use. Guest source, dependency notices and the pinned rebuild command are in [the tokenizer runtime directory](../training/sdk/internal/hftokenizer/).

## Verification

The offline suite covers REST, training/session lifecycles, policy gates, streaming and MLflow. Go and the official Python wheel exchanged tree artifacts. Tokenization comparisons used all nine reviewed Kimi profiles and five full-size DeepSeek/GLM model snapshots. The latter matched Python on 40 encode/decode fixtures and 16 rendered prompts with token IDs. Live Fireworks inference, provisioning, billing and RDMA hardware were outside the offline verification.
