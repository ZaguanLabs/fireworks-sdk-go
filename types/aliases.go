package types

type CompletionCreateParams = CompletionCreateParamsCompletionCreateParamsBase
type CompletionCreateParamsNonStreaming = CompletionCreateParamsCompletionCreateParamsNonStreaming
type CompletionCreateParamsStreaming = CompletionCreateParamsCompletionCreateParamsStreaming

type ChatCompletionCreateParams = ChatCompletionCreateParamsCompletionCreateParamsBase
type ChatCompletionCreateParamsNonStreaming = ChatCompletionCreateParamsCompletionCreateParamsNonStreaming
type ChatCompletionCreateParamsStreaming = ChatCompletionCreateParamsCompletionCreateParamsStreaming

type ChatCompletionChunkChoice = ChatChatCompletionChunkChoice
type ChatCompletionChunkChoiceDelta = ChatChatCompletionChunkChoiceDelta

type SharedParamsDeployedModelRef struct{}

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

type PageInfo struct {
	Params map[string]any
}

type AccountsPage struct {
	Accounts      []Account `json:"accounts"`
	NextPageToken *string   `json:"nextPageToken,omitempty"`
}

func (p *AccountsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *AccountsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type APIKeysPage struct {
	APIKeys       []APIKey `json:"apiKeys"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

func (p *APIKeysPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *APIKeysPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type BatchInferenceJobsPage struct {
	BatchInferenceJobs []BatchInferenceJob `json:"batchInferenceJobs"`
	NextPageToken      *string             `json:"nextPageToken,omitempty"`
}

func (p *BatchInferenceJobsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *BatchInferenceJobsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type DatasetsPage struct {
	Datasets      []Dataset `json:"datasets"`
	NextPageToken *string   `json:"nextPageToken,omitempty"`
}

func (p *DatasetsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *DatasetsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type DeploymentShapeVersionsPage struct {
	DeploymentShapeVersions []DeploymentShapeVersion `json:"deploymentShapeVersions"`
	NextPageToken           *string                  `json:"nextPageToken,omitempty"`
}

func (p *DeploymentShapeVersionsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *DeploymentShapeVersionsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type DeploymentShapesPage struct {
	DeploymentShapes []DeploymentShape `json:"deploymentShapes"`
	NextPageToken    *string           `json:"nextPageToken,omitempty"`
}

func (p *DeploymentShapesPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *DeploymentShapesPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type DeploymentsPage struct {
	Deployments   []Deployment `json:"deployments"`
	NextPageToken *string      `json:"nextPageToken,omitempty"`
}

func (p *DeploymentsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *DeploymentsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type DPOJobsPage struct {
	DPOJobs       []DpoJob `json:"dpoJobs"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

func (p *DPOJobsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *DPOJobsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type EvaluationJobsPage struct {
	EvaluationJobs []EvaluationJobListResponse `json:"evaluationJobs"`
	NextPageToken  *string                     `json:"nextPageToken,omitempty"`
}

func (p *EvaluationJobsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *EvaluationJobsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type EvaluatorsPage struct {
	Evaluators    []EvaluatorListResponse `json:"evaluators"`
	NextPageToken *string                 `json:"nextPageToken,omitempty"`
}

func (p *EvaluatorsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *EvaluatorsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type LoraPage struct {
	DeployedModels []SharedDeployedModel `json:"deployedModels"`
	NextPageToken  *string               `json:"nextPageToken,omitempty"`
}

func (p *LoraPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *LoraPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type ModelsPage struct {
	Models        []Model `json:"models"`
	NextPageToken *string `json:"nextPageToken,omitempty"`
}

func (p *ModelsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *ModelsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type ReinforcementFineTuningJobsPage struct {
	ReinforcementFineTuningJobs []ReinforcementFineTuningJob `json:"reinforcementFineTuningJobs"`
	NextPageToken               *string                      `json:"nextPageToken,omitempty"`
}

func (p *ReinforcementFineTuningJobsPage) HasNextPage() bool { return hasNextPage(p.NextPageToken) }
func (p *ReinforcementFineTuningJobsPage) NextPageInfo() *PageInfo {
	return nextPageInfo(p.NextPageToken)
}

type ReinforcementFineTuningStepsPage struct {
	RLORTrainerJobs []ReinforcementFineTuningStep `json:"rlorTrainerJobs"`
	NextPageToken   *string                       `json:"nextPageToken,omitempty"`
}

func (p *ReinforcementFineTuningStepsPage) HasNextPage() bool { return hasNextPage(p.NextPageToken) }
func (p *ReinforcementFineTuningStepsPage) NextPageInfo() *PageInfo {
	return nextPageInfo(p.NextPageToken)
}

type SecretsPage struct {
	Secrets       []Secret `json:"secrets"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

func (p *SecretsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *SecretsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type SupervisedFineTuningJobsPage struct {
	SupervisedFineTuningJobs []SupervisedFineTuningJob `json:"supervisedFineTuningJobs"`
	NextPageToken            *string                   `json:"nextPageToken,omitempty"`
}

func (p *SupervisedFineTuningJobsPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *SupervisedFineTuningJobsPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

type UsersPage struct {
	Users         []User  `json:"users"`
	NextPageToken *string `json:"nextPageToken,omitempty"`
}

func (p *UsersPage) HasNextPage() bool       { return hasNextPage(p.NextPageToken) }
func (p *UsersPage) NextPageInfo() *PageInfo { return nextPageInfo(p.NextPageToken) }

func hasNextPage(nextPageToken *string) bool {
	return nextPageToken != nil && *nextPageToken != ""
}

func nextPageInfo(nextPageToken *string) *PageInfo {
	if !hasNextPage(nextPageToken) {
		return nil
	}
	return &PageInfo{Params: map[string]any{"pageToken": *nextPageToken}}
}
