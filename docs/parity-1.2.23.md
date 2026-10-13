# Fireworks Python SDK 1.2.23 parity update

Source: official PyPI sdist, SHA-256 `50999343468c342568b44265e0c1ae5956afac4ab1accd583b12c693cf4bbfa1`. Built unmodified with `python -m build`; generated sdist unpacked under ignored `docs/fireworks-py/fireworks_ai-1.2.23/dist/`.

| Upstream changes since 1.2.19 | Go implementation |
| --- | --- |
| DPO/SFT/RFT-step cancellation | Generic and typed account-scoped cancellation, caller body and request options; new parameter aliases |
| DPPO and heterogeneous loss config | `LossFnDPPO`, native `map[string]any`, target-support selector validation and capability gating |
| Per-model target-logprob capability | Strict optional boolean decode; only true enables sampling-support loss |
| Top-K sampling references | Positive top-K control, trainer-owned shared-storage headers, request size helper, storage-domain and token-alignment validation, completion-only references and echo stripping |
| Sampling telemetry | Completion token count, complete-request latency, every attempt error, 65,536-entry ring and separate 32-observation/10-minute pressure history surviving drains |
| Projection provisioning | First-class `trainingConfig.projectionHeadDim`, including shape selection; opaque args no longer synthesize topology |
| Opaque reference/deployment settings | Reference config retains caller args; no policy-output CMEK inference or chart-value-based RDMA eligibility |
| Supervised serverless capacity retry | Explicit native REST opt-in, retrieve-only path and capacity/recovery classification; identity and unrecognized conflicts remain terminal |
| TITO response/replay compatibility | Original tool argument strings retained separately, canonical lineage maintained, stream indices added on deep copies and stripped from replay, artifact mismatch rejection |
| TITO top-K metadata and binding | Compact references retained in turns/artifacts; explicit unicast IP binding, default loopback, IPv6 URL support |

The resource/type catalog reports no missing resource methods or exported type names. Regression tests cover cancellation wire contracts, capability decoding, DPPO requests, storage and alignment failures, strict opt-in capacity retry, bounded telemetry, stale diagnostics, sidecar binding, stream copying, canonical replay and artifact round-trips.

Training tensor differentiation and Tinker implementation details use the established native Go compute/callback/backend interfaces. Applications supply these backends and auxiliary-call handling. `WithServerlessSupervised409Retry` applies to `TrainingRestClient` requests; custom future backends can use `ShouldRetryServerlessCapacity409`. Retries retain the configured Go timeout budget. No live inference or training services were called.
