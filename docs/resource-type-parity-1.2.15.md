# Fireworks SDK Go resource/type parity report (1.2.15)

Python SDK source: `docs/fireworks-py/fireworks_ai-1.2.15/dist/fireworks_ai-1.2.15/src/fireworks`.

## Resource Methods

| Resource | Python methods | Missing in Go | Extra Go methods |
| --- | ---: | --- | --- |
| `APIKeysResource` | 3 | none | none |
| `AccountsResource` | 2 | none | none |
| `BatchInferenceJobsResource` | 4 | none | none |
| `ChatCompletionsResource` | 1 | none | none |
| `CompletionsResource` | 1 | none | none |
| `DPOJobsResource` | 6 | none | none |
| `DatasetsResource` | 9 | none | UploadFile |
| `DeploymentShapeVersionsResource` | 4 | none | none |
| `DeploymentShapesResource` | 2 | none | none |
| `DeploymentsResource` | 7 | none | none |
| `EvaluationJobsResource` | 5 | none | none |
| `EvaluatorsResource` | 9 | none | none |
| `LoraResource` | 5 | none | none |
| `MessagesResource` | 1 | none | none |
| `ModelsResource` | 9 | none | none |
| `ReinforcementFineTuningJobsResource` | 6 | none | none |
| `ReinforcementFineTuningStepsResource` | 6 | none | none |
| `SecretsResource` | 5 | none | none |
| `SupervisedFineTuningJobsResource` | 5 | none | none |
| `UsersResource` | 4 | none | none |

## Type Catalog

- Python classes, exported aliases, and top-level type exports expected: 352
- Go generated/alias type names: 384
- Missing expected Go names: none
- Extra Go names are helper aliases and pagination wrappers.
