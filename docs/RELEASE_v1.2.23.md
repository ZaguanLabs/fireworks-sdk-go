# Fireworks Go SDK v1.2.23

Updates the SDK against official Fireworks Python 1.2.23.

- Adds DPO, supervised-fine-tuning and reinforcement-fine-tuning-step cancellation, with generic and typed methods.
- Adds DPPO loss, target-logprob support selection and strict trainer capability validation.
- Adds Parquet top-K sampling references, trainer-storage headers, completion-token alignment, echo handling and TITO artifact propagation.
- Uses first-class projection-head trainer configuration and preserves opaque caller overrides.
- Records all non-cancelled sampling attempts, with bounded metric storage and recent pressure diagnostics that survive draining.
- Adds explicit supervised serverless capacity-409 retry opt-in, restricted to future retrieval. Dedicated clients and terminal conflicts retain their behavior.
- Preserves client-facing tool argument strings while canonicalizing lineage, isolates streamed tool indices, validates artifact response messages, and supports explicit unicast sidecar binding.

Sources were downloaded, SHA-256 verified, built with `python -m build`, and the generated sdist unpacked. See `docs/parity-1.2.23.md` for provenance and native backend scope.

Validation: complete offline race-tested suite, build, vet, resource/type catalog and diff checks. No live API calls.
