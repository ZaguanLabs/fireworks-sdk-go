# Fireworks Python SDK 1.2.15 parity matrix

Baseline: official PyPI `fireworks_ai` 1.2.15 source distribution, downloaded and SHA-256 verified, built with `python -m build`, and unpacked from the resulting `dist/` archive.

Local source: `docs/fireworks-py/fireworks_ai-1.2.15/dist/fireworks_ai-1.2.15/src/fireworks`.

Source SHA-256: `dcf40ad63aaebcf85c5924785b086e69dcca385a338b7e14b15bfaa7c38d47cb`.

## Delta from 1.2.11

| Upstream behavior | Go implementation |
| --- | --- |
| Deployment shape matching and convenience model lookup | Typed and map resource methods, account resolution |
| Shapeless risk acceptance; obsolete dataset filter removal; checkpoint intervals | Generated types, query serialization, deployment validation |
| SFT/DPO checkpoint listing and promotion | Paginated training client helpers, operation polling |
| Trainer failure lifecycle status | Structured `TrainingAPIError` status retention |
| Reattachment readiness and transition matching | Control-plane polling before patch and until matching READY state |
| Parent shape selection and explicit version pinning | `TrainerCreateShapeRef` |
| Optional trainer-first provisioning | `WaitForTrainerBeforeDeployment`; default overlapping startup |
| Removal of obsolete managed rollout annotation | Managed deployment configuration |
| Per-model comms v1/v2 capabilities | `CreateModelResponse`, `TrainingModelCreator`, request-body serializer |
| Shaped float32 custom gradients | `LinearLossWeights`; sparse tensor index fields |
| Compact Parquet routing references | Validation, slicing, masking, concatenation, file deduplication, compact spans |
| Trainer/inference shared-store negotiation | Lazy shared probe, strict acknowledgement, request headers, positive TTL checks, exact legacy fallback |
| Per-training-model sampler binding | Isolated sampler binding; ambiguous service-level bindings reject R3 requests |
| Partial prompt echo and top-k inference data | Sampling alignment checks and separate prompt/completion routing |
| TITO routing artifacts and admission normalization | Compact-reference JSON round trips; empty assistant refusal normalization only at admission |
| Artifact compression memory improvement | Incremental canonical JSON writes to zlib |
| Removed redundant agent wall observation | Compatibility no-op after trajectory validation |

## Runtime scope

Python's asyncio, Pydantic, NumPy, PyTorch, pyqwest, and Tinker monkeypatches have no direct Go equivalents. Go uses contexts, native JSON/tensor structures, and the existing injectable training backend interfaces. Transport adapters consume the negotiated capability and request options; this release does not replace those adapters with a standalone Tinker transport. Artifact packing still builds an intermediate JSON map, but no longer buffers a second complete canonical JSON document before compression.

The [resource/type catalog](resource-type-parity-1.2.15.md) verifies exported names, not all remote service behavior. Unit and race tests verify the upstream delta using local HTTP fixtures and controlled backends; live service parity was not measured.
