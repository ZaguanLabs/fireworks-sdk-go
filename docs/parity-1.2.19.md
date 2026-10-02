# Fireworks Python SDK 1.2.19 parity update

Baseline: official PyPI source, SHA-256 `0477e76446e46f8b8894bd3c7ecb0754a1eb599ce44446c7a74444a6ca2049cf`. Built with `python -m build`; generated sdist unpacked under `docs/fireworks-py/fireworks_ai-1.2.19/dist/`.

| Upstream change since 1.2.15 | Go implementation |
| --- | --- |
| Multi-shard dataset upload | Sorted and validated JSONL shards, line counting, complete registration, signed HTTPS PUTs using a separate client, URL refresh and final validation |
| Encryption safety | CMEK and unknown encryption states fail before registration/upload |
| Projection heads | Service/model/resume options, trainer scalar flag, create-model wire helper, forward-only reads, shaped custom gradients |
| Per-model LoRA initialization | `InitMethod`/`LoraInitMethod`, default-aware duplicate keys and create-model wire helper |
| Router Replay capability | Nullable strict boolean capability; malformed values remain unknown |
| Model lookup failures | `ModelDetailsUnavailableError` retains HTTP status |
| RDMA weight publication | Dedicated full-parameter eligibility, per-replica negotiation, fan-out/CMEK/LoRA opt-outs, stable session identity, FILE fallback |
| Safe RDMA fallback | Only submission-time 404/405 allows FILE retry; completion errors never retry via FILE |
| Sampler lifecycle | In-flight cancellation with typed error; optional bounded background drain |
| Deployment routing and chart overrides | Deployed base model is retained, trainer `extraValues` forwarded, reference projection topology cleared |

The resource/type catalog reports no missing generated resource methods or type names. Python-specific tensor differentiation and Tinker transport implementations remain represented by native Go callback/backend interfaces, as in the previous release. Custom loss callbacks return gradients explicitly. RDMA adapters implement `TrainingWeightSyncBackend` and must distinguish submission rejection from an accepted operation's completion failure. This is not a live-service parity measurement.
