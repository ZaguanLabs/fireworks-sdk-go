package fireworks

import "time"

type Status struct {
	Code    *int    `json:"code,omitempty"`
	Message *string `json:"message,omitempty"`
	Details []any   `json:"details,omitempty"`
}

type Account struct {
	Email        string     `json:"email"`
	AccountType  *string    `json:"accountType,omitempty"`
	CreateTime   *time.Time `json:"createTime,omitempty"`
	DisplayName  *string    `json:"displayName,omitempty"`
	Name         *string    `json:"name,omitempty"`
	State        *string    `json:"state,omitempty"`
	Status       *Status    `json:"status,omitempty"`
	SuspendState *string    `json:"suspendState,omitempty"`
	UpdateTime   *time.Time `json:"updateTime,omitempty"`
}

type User struct {
	Role           string     `json:"role"`
	CreateTime     *time.Time `json:"createTime,omitempty"`
	DisplayName    *string    `json:"displayName,omitempty"`
	Email          *string    `json:"email,omitempty"`
	Name           *string    `json:"name,omitempty"`
	ServiceAccount *bool      `json:"serviceAccount,omitempty"`
	State          *string    `json:"state,omitempty"`
	Status         *Status    `json:"status,omitempty"`
	UpdateTime     *time.Time `json:"updateTime,omitempty"`
}

type APIKey struct {
	CreateTime  *time.Time `json:"createTime,omitempty"`
	DisplayName *string    `json:"displayName,omitempty"`
	Email       *string    `json:"email,omitempty"`
	ExpireTime  *time.Time `json:"expireTime,omitempty"`
	Key         *string    `json:"key,omitempty"`
	KeyID       *string    `json:"keyId,omitempty"`
	Prefix      *string    `json:"prefix,omitempty"`
	Secure      *bool      `json:"secure,omitempty"`
}

type Secret struct {
	KeyName string  `json:"keyName"`
	Name    string  `json:"name"`
	Value   *string `json:"value,omitempty"`
}

type Model struct {
	BaseModelDetails                   any                `json:"baseModelDetails,omitempty"`
	Calibrated                         *bool              `json:"calibrated,omitempty"`
	Cluster                            *string            `json:"cluster,omitempty"`
	ContextLength                      *int               `json:"contextLength,omitempty"`
	ConversationConfig                 any                `json:"conversationConfig,omitempty"`
	CreateTime                         *time.Time         `json:"createTime,omitempty"`
	DefaultDraftModel                  *string            `json:"defaultDraftModel,omitempty"`
	DefaultDraftTokenCount             *int               `json:"defaultDraftTokenCount,omitempty"`
	DefaultSamplingParams              map[string]float64 `json:"defaultSamplingParams,omitempty"`
	DeployedModelRefs                  []any              `json:"deployedModelRefs,omitempty"`
	DeprecationDate                    any                `json:"deprecationDate,omitempty"`
	Description                        *string            `json:"description,omitempty"`
	DisplayName                        *string            `json:"displayName,omitempty"`
	FineTuningJob                      *string            `json:"fineTuningJob,omitempty"`
	GithubURL                          *string            `json:"githubUrl,omitempty"`
	HuggingFaceURL                     *string            `json:"huggingFaceUrl,omitempty"`
	ImportedFrom                       *string            `json:"importedFrom,omitempty"`
	Kind                               *string            `json:"kind,omitempty"`
	Name                               *string            `json:"name,omitempty"`
	PeftDetails                        any                `json:"peftDetails,omitempty"`
	Public                             *bool              `json:"public,omitempty"`
	RLTunable                          *bool              `json:"rlTunable,omitempty"`
	SnapshotType                       *string            `json:"snapshotType,omitempty"`
	State                              *string            `json:"state,omitempty"`
	Status                             *Status            `json:"status,omitempty"`
	SupportedPrecisions                []string           `json:"supportedPrecisions,omitempty"`
	SupportedPrecisionsWithCalibration []string           `json:"supportedPrecisionsWithCalibration,omitempty"`
	SupportsImageInput                 *bool              `json:"supportsImageInput,omitempty"`
	SupportsLora                       *bool              `json:"supportsLora,omitempty"`
	SupportsServerless                 *bool              `json:"supportsServerless,omitempty"`
	SupportsTools                      *bool              `json:"supportsTools,omitempty"`
	TeftDetails                        any                `json:"teftDetails,omitempty"`
	TrainingContextLength              *int               `json:"trainingContextLength,omitempty"`
	Tunable                            *bool              `json:"tunable,omitempty"`
	UpdateTime                         *time.Time         `json:"updateTime,omitempty"`
	UseHFApplyChatTemplate             *bool              `json:"useHfApplyChatTemplate,omitempty"`
}

type Dataset struct {
	AverageTurnCount    *float64   `json:"averageTurnCount,omitempty"`
	CreatedBy           *string    `json:"createdBy,omitempty"`
	CreateTime          *time.Time `json:"createTime,omitempty"`
	DisplayName         *string    `json:"displayName,omitempty"`
	EstimatedTokenCount *string    `json:"estimatedTokenCount,omitempty"`
	EvalProtocol        any        `json:"evalProtocol,omitempty"`
	EvaluationResult    any        `json:"evaluationResult,omitempty"`
	ExampleCount        *string    `json:"exampleCount,omitempty"`
	ExternalURL         *string    `json:"externalUrl,omitempty"`
	Format              *string    `json:"format,omitempty"`
	Name                *string    `json:"name,omitempty"`
	SourceJobName       *string    `json:"sourceJobName,omitempty"`
	Splitted            any        `json:"splitted,omitempty"`
	State               *string    `json:"state,omitempty"`
	Status              *Status    `json:"status,omitempty"`
	Transformed         any        `json:"transformed,omitempty"`
	UpdateTime          *time.Time `json:"updateTime,omitempty"`
	UserUploaded        any        `json:"userUploaded,omitempty"`
}

type Deployment struct {
	BaseModel             string     `json:"baseModel"`
	AcceleratorCount      *int       `json:"acceleratorCount,omitempty"`
	AcceleratorType       *string    `json:"acceleratorType,omitempty"`
	ActiveModelVersion    *string    `json:"activeModelVersion,omitempty"`
	AutoscalingPolicy     any        `json:"autoscalingPolicy,omitempty"`
	AutoTune              any        `json:"autoTune,omitempty"`
	Cluster               *string    `json:"cluster,omitempty"`
	CreateTime            *time.Time `json:"createTime,omitempty"`
	DeleteTime            *time.Time `json:"deleteTime,omitempty"`
	DeploymentShape       *string    `json:"deploymentShape,omitempty"`
	DeploymentTemplate    *string    `json:"deploymentTemplate,omitempty"`
	Description           *string    `json:"description,omitempty"`
	DesiredReplicaCount   *int       `json:"desiredReplicaCount,omitempty"`
	DisplayName           *string    `json:"displayName,omitempty"`
	DraftModel            *string    `json:"draftModel,omitempty"`
	DraftTokenCount       *int       `json:"draftTokenCount,omitempty"`
	EnableAddons          *bool      `json:"enableAddons,omitempty"`
	EnableHotLoad         *bool      `json:"enableHotLoad,omitempty"`
	EnableMTP             *bool      `json:"enableMtp,omitempty"`
	EnableSessionAffinity *bool      `json:"enableSessionAffinity,omitempty"`
	Name                  *string    `json:"name,omitempty"`
	Public                *bool      `json:"public,omitempty"`
	ReplicaStats          any        `json:"replicaStats,omitempty"`
	State                 *string    `json:"state,omitempty"`
	Status                *Status    `json:"status,omitempty"`
	UpdateTime            *time.Time `json:"updateTime,omitempty"`
}

type DeploymentShape struct {
	BaseModel                       string     `json:"baseModel"`
	AcceleratorCount                *int       `json:"acceleratorCount,omitempty"`
	AcceleratorType                 *string    `json:"acceleratorType,omitempty"`
	CreateTime                      *time.Time `json:"createTime,omitempty"`
	Description                     *string    `json:"description,omitempty"`
	DisableDeploymentSizeValidation *bool      `json:"disableDeploymentSizeValidation,omitempty"`
	DisplayName                     *string    `json:"displayName,omitempty"`
	DraftModel                      *string    `json:"draftModel,omitempty"`
	DraftTokenCount                 *int       `json:"draftTokenCount,omitempty"`
	EnableAddons                    *bool      `json:"enableAddons,omitempty"`
	EnableSessionAffinity           *bool      `json:"enableSessionAffinity,omitempty"`
	MaxContextLength                *int       `json:"maxContextLength,omitempty"`
	ModelType                       *string    `json:"modelType,omitempty"`
	Name                            *string    `json:"name,omitempty"`
	ParameterCount                  *string    `json:"parameterCount,omitempty"`
	Precision                       *string    `json:"precision,omitempty"`
	PresetType                      *string    `json:"presetType,omitempty"`
	UpdateTime                      *time.Time `json:"updateTime,omitempty"`
}

type DeploymentShapeVersion struct {
	CreateTime      *time.Time       `json:"createTime,omitempty"`
	LatestValidated *bool            `json:"latestValidated,omitempty"`
	Name            *string          `json:"name,omitempty"`
	Public          *bool            `json:"public,omitempty"`
	Snapshot        *DeploymentShape `json:"snapshot,omitempty"`
	Validated       *bool            `json:"validated,omitempty"`
}

type DeployedModel struct {
	CreateTime  *time.Time `json:"createTime,omitempty"`
	Default     *bool      `json:"default,omitempty"`
	Deployment  *string    `json:"deployment,omitempty"`
	Description *string    `json:"description,omitempty"`
	DisplayName *string    `json:"displayName,omitempty"`
	Model       *string    `json:"model,omitempty"`
	Name        *string    `json:"name,omitempty"`
	Public      *bool      `json:"public,omitempty"`
	Serverless  *bool      `json:"serverless,omitempty"`
	State       *string    `json:"state,omitempty"`
	Status      *Status    `json:"status,omitempty"`
	UpdateTime  *time.Time `json:"updateTime,omitempty"`
}

type BatchInferenceJob struct {
	ContinuedFromJobName *string    `json:"continuedFromJobName,omitempty"`
	CreatedBy            *string    `json:"createdBy,omitempty"`
	CreateTime           *time.Time `json:"createTime,omitempty"`
	DisplayName          *string    `json:"displayName,omitempty"`
	InferenceParameters  any        `json:"inferenceParameters,omitempty"`
	InputDatasetID       *string    `json:"inputDatasetId,omitempty"`
	JobProgress          any        `json:"jobProgress,omitempty"`
	Model                *string    `json:"model,omitempty"`
	Name                 *string    `json:"name,omitempty"`
	OutputDatasetID      *string    `json:"outputDatasetId,omitempty"`
	Precision            *string    `json:"precision,omitempty"`
	State                *string    `json:"state,omitempty"`
	Status               *Status    `json:"status,omitempty"`
	UpdateTime           *time.Time `json:"updateTime,omitempty"`
}

type FineTuningJob struct {
	Dataset              string     `json:"dataset"`
	BaseModel            *string    `json:"baseModel,omitempty"`
	CreatedBy            *string    `json:"createdBy,omitempty"`
	CreateTime           *time.Time `json:"createTime,omitempty"`
	DisplayName          *string    `json:"displayName,omitempty"`
	EvaluationDataset    *string    `json:"evaluationDataset,omitempty"`
	JobProgress          any        `json:"jobProgress,omitempty"`
	MetricsFileSignedURL *string    `json:"metricsFileSignedUrl,omitempty"`
	Name                 *string    `json:"name,omitempty"`
	State                *string    `json:"state,omitempty"`
	Status               *Status    `json:"status,omitempty"`
	TrainerLogsSignedURL *string    `json:"trainerLogsSignedUrl,omitempty"`
	TrainingConfig       any        `json:"trainingConfig,omitempty"`
	UpdateTime           *time.Time `json:"updateTime,omitempty"`
	WandbConfig          any        `json:"wandbConfig,omitempty"`
}

type SupervisedFineTuningJob = FineTuningJob
type ReinforcementFineTuningJob = FineTuningJob
type ReinforcementFineTuningStep = FineTuningJob
type DPOJob = FineTuningJob

type EvaluationJob struct {
	Evaluator     string             `json:"evaluator"`
	InputDataset  string             `json:"inputDataset"`
	OutputDataset string             `json:"outputDataset"`
	CreatedBy     *string            `json:"createdBy,omitempty"`
	CreateTime    *time.Time         `json:"createTime,omitempty"`
	DisplayName   *string            `json:"displayName,omitempty"`
	Metrics       map[string]float64 `json:"metrics,omitempty"`
	Name          *string            `json:"name,omitempty"`
	OutputStats   *string            `json:"outputStats,omitempty"`
	State         *string            `json:"state,omitempty"`
	Status        *Status            `json:"status,omitempty"`
	UpdateTime    *time.Time         `json:"updateTime,omitempty"`
}

type Evaluator struct {
	CommitHash     *string    `json:"commitHash,omitempty"`
	CreatedBy      *string    `json:"createdBy,omitempty"`
	CreateTime     *time.Time `json:"createTime,omitempty"`
	Criteria       []any      `json:"criteria,omitempty"`
	DefaultDataset *string    `json:"defaultDataset,omitempty"`
	Description    *string    `json:"description,omitempty"`
	DisplayName    *string    `json:"displayName,omitempty"`
	EntryPoint     *string    `json:"entryPoint,omitempty"`
	Name           *string    `json:"name,omitempty"`
	Requirements   *string    `json:"requirements,omitempty"`
	Source         any        `json:"source,omitempty"`
	State          *string    `json:"state,omitempty"`
	Status         *Status    `json:"status,omitempty"`
	UpdateTime     *time.Time `json:"updateTime,omitempty"`
}

type SignedURLEndpoint struct {
	FilenameToSignedURLs   map[string]string `json:"filenameToSignedUrls,omitempty"`
	FilenameToUnsignedURIs map[string]string `json:"filenameToUnsignedUris,omitempty"`
}

type DatasetUploadResponse struct {
	ID        *string `json:"id,omitempty"`
	Bytes     *int    `json:"bytes,omitempty"`
	CreatedAt *int64  `json:"created_at,omitempty"`
	Filename  *string `json:"filename,omitempty"`
	Object    *string `json:"object,omitempty"`
	Purpose   *string `json:"purpose,omitempty"`
}

type EvaluatorBuildLogEndpoint struct {
	BuildLogSignedURI *string `json:"buildLogSignedUri,omitempty"`
}

type ModelValidateUploadResponse struct {
	Warnings []string `json:"warnings,omitempty"`
}

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
	DPOJobs       []DPOJob `json:"dpoJobs"`
	NextPageToken *string  `json:"nextPageToken,omitempty"`
}

type EvaluationJobsPage struct {
	EvaluationJobs []EvaluationJob `json:"evaluationJobs"`
	NextPageToken  *string         `json:"nextPageToken,omitempty"`
}

type EvaluatorsPage struct {
	Evaluators    []Evaluator `json:"evaluators"`
	NextPageToken *string     `json:"nextPageToken,omitempty"`
}

type LoraPage struct {
	DeployedModels []DeployedModel `json:"deployedModels"`
	NextPageToken  *string         `json:"nextPageToken,omitempty"`
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
