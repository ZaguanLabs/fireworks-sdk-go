# Fireworks Python SDK 1.2.11 parity matrix

Baseline: `fireworks_ai` `1.2.11` from
`docs/fireworks-py/python-sdk-1.2.11/src/fireworks` and its unpacked built
source distribution.

## Python 1.2.9 to 1.2.11 delta

| Python area | Go area | Status |
| --- | --- | --- |
| DPO, RFT, and SFT `reservation_target` | generated REST types, trainer, and managed lifecycle | ported |
| Explicit merged-base export precision with source default | snapshot chain and weight syncer | ported |
| Trainer readiness termination on stopped jobs | trainer polling | ported |
| Canonical Lifecycle, Tinker, and serverless gateway classifications | structured training and sampling errors | ported |
| Response-header plus final-body serving metrics and request attribution | deployment sampler | ported |
| Gateway congestion signals and retry-exhaustion pressure | adaptive concurrency controller | ported |
| Environment-local exact-token TITO sidecar, artifacts, renderers, drift policy, and incremental prompt contract | `training/sdk/tito` | ported |

The TITO package uses Go's synchronous `context.Context` conventions while
preserving the Python SDK's exact-token transaction boundaries, per-trajectory
credentials, full-history default, experimental incremental renderer opt-in,
idempotency replay, compact artifact codec, and OpenAI-compatible loopback
surface.

The generated resource/type report is checked in at
`docs/resource-type-parity-1.2.11.md`.
