# Fireworks Python SDK 1.2.30 update

Source: official PyPI sdist `fireworks_ai-1.2.30.tar.gz`, SHA-256 `1f58f97a2c8ab7bc649e46983e4a2172111ab3add5cb98a87350daa4c1d035ad`. Built with `python -m build`; generated source archive unpacked under ignored `docs/fireworks-py/fireworks_ai-1.2.30/dist/`.

| Upstream change since 1.2.23 | Go implementation |
| --- | --- |
| Owned training protocol | Native REST model creation, future polling, forward/backward, optimizer steps, checkpoints, state/adapter loading, weight sync and unloading; generated wire type catalog |
| REINFORCE and policy loss capabilities | Loss name and strict optional flags; policy centering, importance sampling and diagnostic capability gates |
| Sampling-mask channel | Parquet request negotiation, credential-free storage bindings, completion-only references, alignment validation and model-input helpers |
| Forward-only operation | Synchronous and future helpers with the same admission and unload lifecycle |
| Checkpoint metadata | Decision calibration validation/defaults and sampler metadata; unsupported TTL options rejected before submission |
| Sampler handles | Credential-free serialization, receiving-process API key and Cloudflare credentials, routing/context/concurrency restoration |
| Lifecycle | Future admission reserved before submission; unload rejects new work, waits for admitted work and permits cleanup retries; managed cleanup attempts every resource |
| RDMA publication | All required capabilities and explicit enablement must agree; required RDMA failures remain errors rather than falling back to file publication |
| Stream failures | Strict SSE structure and successful finish requirements; bounded upstream attribution; only transient upstream failures retry |
| Serverless sampling | Existing-session checkpoint routing, standalone base samplers, caller/Cloudflare header propagation, session deletion and dedicated capability fetching |
| TITO | Parser errors poison trajectories, failure-origin records, no successful-prefix terminalization, mask artifacts and private bounded failure traces with secure collected-file reads |

The [resource/type catalog](resource-type-parity-1.2.30.md) reports no missing generated REST resource methods or exported names. `training/types` adds the owned protocol's public wire models. The native training runtime removes the need for a Python/Tinker transport bridge for those service operations. Custom tensor differentiation still uses Go callbacks returning explicit gradients; application-specific tensor/device and optional media integrations remain native backend interfaces.

TITO traces default to a private temporary directory and are removed after successful terminalization. Failed traces survive within per-writer size/count limits; credential fields are sanitized. Secure local trace storage currently uses Linux owner/no-follow primitives; other platforms keep the SDK available and decline trace capture. Traces may contain prompts, sampled text and token IDs and are never uploaded automatically. Disable capture with `FailureTraceConfig.Enabled=false` while retaining valid limits.

Validation: full offline race tests, vet and build, including wire fixtures, future polling, model sequencing, metadata, unload/drain/retry, strict stream errors, sampler credential isolation, sampling-mask alignment, poisoned trajectories, trace bounds and no-follow collected reads. Live inference/training tests remain opt-in and were not run.
