# Fireworks Go SDK v1.2.19

Updates the SDK against official Fireworks Python 1.2.19.

- Multi-file JSONL dataset upload with encrypted-dataset rejection, signed-URL validation and refresh, credential-free uploads, and final validation.
- Projection-head configuration, checkpoint-resume metadata, forward-only projection reads, shaped custom-loss gradients, and LoRA initialization selection.
- Strict trainer capability decoding and typed model-lookup errors.
- RDMA weight-sync negotiation for eligible dedicated full-parameter deployments, with FILE fallback for older runtimes. Only rejected 404/405 submissions permit fallback.
- Typed cancellation of in-flight sampling and optional background draining.
- Deployed base-model routing and explicit trainer chart overrides.

Training uses the existing native Go backend interfaces. RDMA transports implement `TrainingWeightSyncBackend`; custom loss callbacks return gradients explicitly. See `docs/parity-1.2.19.md` for scope and source provenance.

Validation: race-tested offline suite, build, vet, resource/type catalog, and diff checks. Live training and inference services were not called.
