# Fireworks Python SDK 1.2.9 parity matrix

Baseline: `fireworks_ai` `1.2.9` from
`docs/fireworks-py/python-sdk-1.2.9/src/fireworks` and its unpacked built source distribution.

## Python 1.2.6 to 1.2.9 delta

| Python area | Go area | Status |
| --- | --- | --- |
| DPO/SFT `renderer_hugging_face_repo_id` and `use_reservation` fields | generated REST types | ported |
| Reservation-first training configuration with explicit opt-out | `training/sdk/trainer.go`, `managed.go`, `managed_lifecycle.go` | ported |
| Policy-output CMEK read from trainer extra arguments | `training/sdk/managed.go`, `managed_lifecycle.go` | ported |
| Generated resource and exported type catalog | `types/generated.go` | verified, no missing names |

Go keeps legacy reference metadata readable for compatibility, while new policy-output
CMEK selection follows Python 1.2.9's `--cmek-output-model-resource` trainer argument.
The static session-affinity header remains in the direct Go sampler because it preserves
the Python transport's observable routing behavior without its internal wrapper layer.

The generated report is checked in at `docs/resource-type-parity-1.2.9.md`.
