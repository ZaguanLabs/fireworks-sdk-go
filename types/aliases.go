package types

type CompletionCreateParams = CompletionCreateParamsCompletionCreateParamsBase
type CompletionCreateParamsNonStreaming = CompletionCreateParamsCompletionCreateParamsNonStreaming
type CompletionCreateParamsStreaming = CompletionCreateParamsCompletionCreateParamsStreaming

type ChatCompletionCreateParams = ChatCompletionCreateParamsCompletionCreateParamsBase
type ChatCompletionCreateParamsNonStreaming = ChatCompletionCreateParamsCompletionCreateParamsNonStreaming
type ChatCompletionCreateParamsStreaming = ChatCompletionCreateParamsCompletionCreateParamsStreaming

type ChatCompletionChunkChoice = ChatChatCompletionChunkChoice
type ChatCompletionChunkChoiceDelta = ChatChatCompletionChunkChoiceDelta

type DPOJob = DPOJobDpoJob
type DpoJob = DPOJobDpoJob
type DPOJobCreateParams = DPOJobCreateParamsDpoJobCreateParams
type DpoJobCreateParams = DPOJobCreateParamsDpoJobCreateParams
type DPOJobGetParams = DPOJobGetParamsDpoJobGetParams
type DpoJobGetParams = DPOJobGetParamsDpoJobGetParams
type DPOJobListParams = DPOJobListParamsDpoJobListParams
type DpoJobListParams = DPOJobListParamsDpoJobListParams
type DPOJobResumeParams = DPOJobResumeParamsDpoJobResumeParams
type DpoJobResumeParams = DPOJobResumeParamsDpoJobResumeParams
type DPOJobGetMetricsFileEndpointResponse = DPOJobGetMetricsFileEndpointResponseDpoJobGetMetricsFileEndpointResponse
type DpoJobGetMetricsFileEndpointResponse = DPOJobGetMetricsFileEndpointResponseDpoJobGetMetricsFileEndpointResponse

type AccountsPage struct {
	Accounts      []Account `json:"accounts"`
	NextPageToken *string   `json:"nextPageToken,omitempty"`
}

type APIKeysPage struct {
	APIKeys       []APIKey `json:"apiKeys"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

type BatchInferenceJobsPage struct {
	BatchInferenceJobs []BatchInferenceJob `json:"batchInferenceJobs"`
	NextPageToken      *string             `json:"nextPageToken,omitempty"`
}

type DatasetsPage struct {
	Datasets      []Dataset `json:"datasets"`
	NextPageToken *string   `json:"nextPageToken,omitempty"`
}

type DeploymentShapeVersionsPage struct {
	DeploymentShapeVersions []DeploymentShapeVersion `json:"deploymentShapeVersions"`
	NextPageToken           *string                  `json:"nextPageToken,omitempty"`
}

type DeploymentShapesPage struct {
	DeploymentShapes []DeploymentShape `json:"deploymentShapes"`
	NextPageToken    *string           `json:"nextPageToken,omitempty"`
}

type DeploymentsPage struct {
	Deployments   []Deployment `json:"deployments"`
	NextPageToken *string      `json:"nextPageToken,omitempty"`
}

type DPOJobsPage struct {
	DPOJobs       []DpoJob `json:"dpoJobs"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

type EvaluationJobsPage struct {
	EvaluationJobs []EvaluationJobListResponse `json:"evaluationJobs"`
	NextPageToken  *string                     `json:"nextPageToken,omitempty"`
}

type EvaluatorsPage struct {
	Evaluators    []EvaluatorListResponse `json:"evaluators"`
	NextPageToken *string                 `json:"nextPageToken,omitempty"`
}

type LoraPage struct {
	DeployedModels []SharedDeployedModel `json:"deployedModels"`
	NextPageToken  *string               `json:"nextPageToken,omitempty"`
}

type ModelsPage struct {
	Models        []Model `json:"models"`
	NextPageToken *string `json:"nextPageToken,omitempty"`
}

type ReinforcementFineTuningJobsPage struct {
	ReinforcementFineTuningJobs []ReinforcementFineTuningJob `json:"reinforcementFineTuningJobs"`
	NextPageToken               *string                      `json:"nextPageToken,omitempty"`
}

type ReinforcementFineTuningStepsPage struct {
	RLORTrainerJobs []ReinforcementFineTuningStep `json:"rlorTrainerJobs"`
	NextPageToken   *string                       `json:"nextPageToken,omitempty"`
}

type SecretsPage struct {
	Secrets       []Secret `json:"secrets"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

type SupervisedFineTuningJobsPage struct {
	SupervisedFineTuningJobs []SupervisedFineTuningJob `json:"supervisedFineTuningJobs"`
	NextPageToken            *string                   `json:"nextPageToken,omitempty"`
}

type UsersPage struct {
	Users         []User  `json:"users"`
	NextPageToken *string `json:"nextPageToken,omitempty"`
}
