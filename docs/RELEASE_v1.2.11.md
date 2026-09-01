# Fireworks SDK Go v1.2.11

This release aligns the Go SDK with the official Fireworks Python SDK 1.2.11.

## Highlights

- Adds explicit reservation targets to DPO, RFT, SFT, trainer, and managed training flows.
- Adds merged-base export precision selection, defaulting to source precision.
- Stops trainer readiness polling promptly when a job reaches a terminal state.
- Preserves canonical Lifecycle, Tinker, and serverless gateway error classifications.
- Attributes completion serving attempts, correlation headers, final performance metrics, and congestion signals.
- Adds the environment-local `training/sdk/tito` package for exact-token multi-turn rollouts.

See `docs/parity-1.2.11.md` for the complete upstream delta.
