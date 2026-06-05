package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	protoDurationRE = regexp.MustCompile(`^-?\d+(\.\d{1,9})?s$`)
	shapeRefRE      = regexp.MustCompile(`^accounts/[^/]+/trainingShapes/[^/]+(/versions/[^/]+)?$`)
)

type TrainerServiceEndpoint struct {
	JobName string
	JobID   string
	BaseURL string
}

type CreatedTrainerJob struct {
	JobName string
	JobID   string
}

type TrainerJobConfig struct {
	BaseModel                 string
	LoraRank                  int
	MaxContextLength          *int
	LearningRate              float64
	GradientAccumulationSteps *int
	NodeCount                 *int
	TrainerReplicaCount       *int
	DisplayName               string
	HotLoadDeploymentID       string
	Region                    string
	CustomImageTag            string
	ExtraArgs                 []string
	AcceleratorType           string
	AcceleratorCount          *int
	TrainingShapeRef          string
	ForwardOnly               bool
	InactivityTimeout         any
	DisableInactivityCleanup  bool
	SkipValidations           bool
	Purpose                   string
	ManagedBy                 string
}

func (c TrainerJobConfig) Validate() error {
	var errors []string
	if c.BaseModel == "" {
		errors = append(errors, "base_model is required")
	}
	if c.GradientAccumulationSteps != nil && *c.GradientAccumulationSteps != 1 {
		errors = append(errors, "gradient_accumulation_steps must be 1. Server-side gradient accumulation is deprecated on the Tinker/RLOR path.")
	}
	if c.InactivityTimeout != nil {
		if _, err := FormatProtoDuration(c.InactivityTimeout); err != nil {
			errors = append(errors, "inactivity_timeout "+err.Error())
		}
	}
	if c.TrainingShapeRef != "" {
		if c.AcceleratorType != "" {
			errors = append(errors, shapeOwnedFieldError("accelerator_type"))
		}
		if c.AcceleratorCount != nil && *c.AcceleratorCount != 0 {
			errors = append(errors, shapeOwnedFieldError("accelerator_count"))
		}
		if c.CustomImageTag != "" {
			errors = append(errors, shapeOwnedFieldError("custom_image_tag"))
		}
		if c.NodeCount != nil && *c.NodeCount != 0 {
			errors = append(errors, shapeOwnedFieldError("node_count"))
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "\n"))
	}
	return nil
}

func FormatProtoDuration(value any) (string, error) {
	switch v := value.(type) {
	case time.Duration:
		if v < 0 {
			return "", fmt.Errorf("must be non-negative")
		}
		if v%time.Second == 0 {
			return fmt.Sprintf("%ds", int64(v/time.Second)), nil
		}
		seconds := float64(v) / float64(time.Second)
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.9f", seconds), "0"), ".") + "s", nil
	case string:
		if !protoDurationRE.MatchString(v) {
			return "", fmt.Errorf("must be a protobuf JSON duration string such as '1800s'; use time.Duration for minute/hour values")
		}
		if strings.HasPrefix(v, "-") {
			return "", fmt.Errorf("must be non-negative")
		}
		return v, nil
	default:
		return "", fmt.Errorf("must be time.Duration or protobuf JSON duration string")
	}
}

type TrainerJobManager struct {
	*FireworksClient
	BootTime time.Duration
}

func NewTrainerJobManager(apiKey, baseURL string, opts ...TrainingRestClientOption) *TrainerJobManager {
	return &TrainerJobManager{FireworksClient: NewFireworksClient(apiKey, baseURL, opts...)}
}

func (m *TrainerJobManager) Create(ctx context.Context, config TrainerJobConfig) (CreatedTrainerJob, error) {
	body, err := m.CreateRaw(ctx, config)
	if err != nil {
		return CreatedTrainerJob{}, err
	}
	name, _ := body["name"].(string)
	if name == "" {
		return CreatedTrainerJob{}, fmt.Errorf("trainer create response did not include a name")
	}
	return CreatedTrainerJob{JobName: name, JobID: lastPathSegment(name)}, nil
}

func (m *TrainerJobManager) CreateAndWait(ctx context.Context, config TrainerJobConfig, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	created, err := m.Create(ctx, config)
	if err != nil {
		return TrainerServiceEndpoint{}, err
	}
	return m.WaitForReady(ctx, created.JobID, created.JobName, opts...)
}

func (m *TrainerJobManager) WaitForReady(ctx context.Context, jobID, jobName string, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	if jobName == "" {
		accountID, err := m.AccountID(ctx)
		if err != nil {
			return TrainerServiceEndpoint{}, err
		}
		jobName = "accounts/" + accountID + "/rlorTrainerJobs/" + jobID
	}
	return m.PollUntilReady(ctx, jobID, jobName, opts...)
}

func (m *TrainerJobManager) WaitForExisting(ctx context.Context, jobID string, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	return m.WaitForReady(ctx, jobID, "", opts...)
}

func (m *TrainerJobManager) ResumeAndWait(ctx context.Context, jobID string, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	if _, err := m.Resume(ctx, jobID); err != nil {
		return TrainerServiceEndpoint{}, err
	}
	return m.WaitForExisting(ctx, jobID, opts...)
}

type TrainerReconnectOptions struct {
	PollOptions         TrainerPollOptions
	MaxWaitForResumable time.Duration
	Now                 func() time.Time
	Sleep               func(time.Duration)
	GetJob              func(context.Context, string) (map[string]any, error)
	ResumeAndWait       func(context.Context, string, TrainerPollOptions) (TrainerServiceEndpoint, error)
	WaitForExisting     func(context.Context, string, TrainerPollOptions) (TrainerServiceEndpoint, error)
}

func (m *TrainerJobManager) ReconnectAndWait(ctx context.Context, jobID string, opts ...TrainerReconnectOptions) (TrainerServiceEndpoint, error) {
	opt := TrainerReconnectOptions{
		MaxWaitForResumable: ResumableWaitTimeout,
		Now:                 time.Now,
		Sleep:               time.Sleep,
		GetJob:              m.GetJob,
		ResumeAndWait: func(ctx context.Context, jobID string, poll TrainerPollOptions) (TrainerServiceEndpoint, error) {
			return m.ResumeAndWait(ctx, jobID, poll)
		},
		WaitForExisting: func(ctx context.Context, jobID string, poll TrainerPollOptions) (TrainerServiceEndpoint, error) {
			return m.WaitForExisting(ctx, jobID, poll)
		},
	}
	if len(opts) > 0 {
		provided := opts[0]
		opt.PollOptions = provided.PollOptions
		if provided.MaxWaitForResumable != 0 {
			opt.MaxWaitForResumable = provided.MaxWaitForResumable
		}
		if provided.Now != nil {
			opt.Now = provided.Now
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
		if provided.GetJob != nil {
			opt.GetJob = provided.GetJob
		}
		if provided.ResumeAndWait != nil {
			opt.ResumeAndWait = provided.ResumeAndWait
		}
		if provided.WaitForExisting != nil {
			opt.WaitForExisting = provided.WaitForExisting
		}
	}

	start := opt.Now()
	for {
		job, err := opt.GetJob(ctx, jobID)
		if err != nil {
			if opt.Now().Sub(start) > opt.MaxWaitForResumable {
				return TrainerServiceEndpoint{}, err
			}
			opt.Sleep(PollInterval)
			continue
		}
		state := stringFromAny(job["state"])
		if state == "JOB_STATE_RUNNING" {
			return opt.WaitForExisting(ctx, jobID, opt.PollOptions)
		}
		switch state {
		case "JOB_STATE_FAILED", "JOB_STATE_CANCELLED", "JOB_STATE_PAUSED", "JOB_STATE_COMPLETED":
			return opt.ResumeAndWait(ctx, jobID, opt.PollOptions)
		}
		if opt.Now().Sub(start) > opt.MaxWaitForResumable {
			return TrainerServiceEndpoint{}, fmt.Errorf("%s", FormatSDKError(
				"Trainer job "+jobID+" stuck in "+state,
				fmt.Sprintf("Job has been in %q state for %.0fs without transitioning to a resumable state.", state, opt.MaxWaitForResumable.Seconds()),
				"Check the Fireworks console for job details. If the job will not reach a resumable state, cancel it and create a new one.\n  Console: "+ConsoleURL,
				SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
			))
		}
		opt.Sleep(SlowPollInterval)
	}
}

func (m *TrainerJobManager) GetJob(ctx context.Context, jobID string) (map[string]any, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := m.Get(ctx, "/v1/accounts/"+accountID+"/rlorTrainerJobs/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get trainer job %s: HTTP %d: %s", jobID, resp.StatusCode, ParseAPIErrorBody(body))
	}
	var out map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *TrainerJobManager) DeleteJob(ctx context.Context, jobID string) error {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return err
	}
	resp, err := m.Delete(ctx, "/v1/accounts/"+accountID+"/rlorTrainerJobs/"+jobID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete trainer job %s: HTTP %d: %s", jobID, resp.StatusCode, ParseAPIErrorBody(body))
	}
	return nil
}

func (m *TrainerJobManager) Resume(ctx context.Context, jobID string) (map[string]any, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := m.Post(ctx, "/v1/accounts/"+accountID+"/rlorTrainerJobs/"+jobID+":resume", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("resume trainer job %s: HTTP %d: %s", jobID, resp.StatusCode, ParseAPIErrorBody(body))
	}
	var out map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *TrainerJobManager) TrainerGatewayURL(ctx context.Context, jobID string) (string, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return "", err
	}
	return m.BaseURL() + "/training/v1/rlorTrainerJobs/" + accountID + "/" + jobID, nil
}

type TrainerPollOptions struct {
	PollInterval time.Duration
	Timeout      time.Duration
	Now          func() time.Time
	Sleep        func(time.Duration)
	HealthCheck  func(context.Context, string) bool
	GetJob       func(context.Context, string) (map[string]any, error)
}

func (m *TrainerJobManager) PollUntilReady(ctx context.Context, jobID, jobName string, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	opt := TrainerPollOptions{
		PollInterval: PollInterval,
		Timeout:      TrainerReadyTimeout,
		Now:          time.Now,
		Sleep:        time.Sleep,
		HealthCheck:  m.CheckHealthz,
		GetJob:       m.GetJob,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.PollInterval != 0 {
			opt.PollInterval = provided.PollInterval
		}
		if provided.Timeout != 0 {
			opt.Timeout = provided.Timeout
		}
		if provided.Now != nil {
			opt.Now = provided.Now
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
		if provided.HealthCheck != nil {
			opt.HealthCheck = provided.HealthCheck
		}
		if provided.GetJob != nil {
			opt.GetJob = provided.GetJob
		}
	}

	start := opt.Now()
	baseURL, err := m.TrainerGatewayURL(ctx, jobID)
	if err != nil {
		return TrainerServiceEndpoint{}, err
	}
	for opt.Now().Sub(start) < opt.Timeout {
		job, err := opt.GetJob(ctx, jobID)
		if err != nil {
			return TrainerServiceEndpoint{}, err
		}
		state := stringFromAny(job["state"])
		statusMessage := ExtractJobStatusMessage(job)
		if state == "JOB_STATE_FAILED" {
			if statusMessage == "" {
				statusMessage = "unknown"
			}
			return TrainerServiceEndpoint{}, fmt.Errorf("%s", FormatSDKError(
				"Trainer job "+jobID+" failed",
				statusMessage,
				"The trainer status detail above is from the control plane. Check trainer logs and events in the Fireworks console before retrying.\n  Console: "+ConsoleURL,
				SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
			))
		}
		if state == "JOB_STATE_RUNNING" && opt.HealthCheck(ctx, baseURL) {
			m.BootTime = opt.Now().Sub(start)
			return TrainerServiceEndpoint{JobName: jobName, JobID: jobID, BaseURL: baseURL}, nil
		}
		opt.Sleep(opt.PollInterval)
	}
	return TrainerServiceEndpoint{}, fmt.Errorf("%s", FormatSDKError(
		fmt.Sprintf("Trainer job %s did not become ready within %.0fs", jobID, opt.Timeout.Seconds()),
		"The job did not reach JOB_STATE_RUNNING with a healthy /api/v1/healthz response before the timeout.",
		fmt.Sprintf("Increase the trainer ready timeout (current: %.0fs) and check job status in the Fireworks console: %s", opt.Timeout.Seconds(), ConsoleURL),
		SDKErrorFormatOptions{DocsURL: DocsSDK},
	))
}

func (m *TrainerJobManager) CheckHealthz(ctx context.Context, baseURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/healthz", nil)
	if err != nil {
		return false
	}
	req.Header = m.Headers(map[string]string{"Authorization": "Bearer " + m.APIKey()})
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (m *TrainerJobManager) CreateRaw(ctx context.Context, config TrainerJobConfig) (map[string]any, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.TrainingShapeRef != "" {
		if err := ValidateTrainingShapeRef(config.TrainingShapeRef); err != nil {
			return nil, err
		}
	}
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	path := "/v1/accounts/" + accountID + "/rlorTrainerJobs"
	query := url.Values{}
	if config.HotLoadDeploymentID != "" {
		query.Set("deploymentId", config.HotLoadDeploymentID)
	}
	if config.TrainingShapeRef != "" {
		query.Set("trainingShape", config.TrainingShapeRef)
		if config.SkipValidations {
			query.Set("skipValidations", "true")
		}
	} else {
		query.Set("skipValidations", "true")
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	resp, err := m.Post(ctx, path, BuildTrainerCreatePayload(config), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", FormatSDKError(
			fmt.Sprintf("RLOR job creation failed (HTTP %d)", resp.StatusCode),
			ParseAPIErrorBody(body),
			HTTPStatusHints[resp.StatusCode],
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	var out map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func ValidateTrainingShapeRef(ref string) error {
	if shapeRefRE.MatchString(ref) {
		return nil
	}
	return fmt.Errorf("%s", FormatSDKError(
		"Invalid training_shape_ref format",
		fmt.Sprintf("%q is not a valid training shape resource name.", ref),
		"Expected: accounts/<account>/trainingShapes/<shape>[/versions/<version>]\n  Use resolve_training_profile(<short_id>) to get the full resource name:\n    profile = mgr.resolve_training_profile('my-shape')\n    config = TrainerJobConfig(..., training_shape_ref=profile.training_shape_version)",
	))
}

func ExtractJobStatusMessage(job map[string]any) string {
	for _, key := range []string{"statusMessage", "message"} {
		if value := stringFromAny(job[key]); value != "" {
			return value
		}
	}
	status := job["status"]
	if statusMap, ok := status.(map[string]any); ok {
		for _, key := range []string{"message", "statusMessage", "detail", "details"} {
			if value := stringFromAny(statusMap[key]); value != "" {
				return value
			}
		}
		return ""
	}
	return stringFromAny(status)
}

func BuildTrainerCreatePayload(config TrainerJobConfig) map[string]any {
	isShapePath := config.TrainingShapeRef != ""
	learningRate := config.LearningRate
	if learningRate == 0 {
		learningRate = 1e-5
	}
	trainingConfig := map[string]any{
		"baseModel":    config.BaseModel,
		"loraRank":     config.LoraRank,
		"learningRate": learningRate,
	}
	payload := map[string]any{
		"serviceMode":    true,
		"keepAlive":      false,
		"dataset":        "",
		"trainingConfig": trainingConfig,
	}
	if config.TrainerReplicaCount != nil {
		payload["trainerReplicaCount"] = *config.TrainerReplicaCount
	}
	if !isShapePath {
		if config.MaxContextLength != nil {
			trainingConfig["maxContextLength"] = *config.MaxContextLength
		}
		nodeCount := 1
		if config.NodeCount != nil {
			nodeCount = *config.NodeCount
		}
		payload["nodeCount"] = nodeCount
		if config.CustomImageTag != "" {
			trainingConfig["customImageTag"] = config.CustomImageTag
		}
		if config.AcceleratorType != "" {
			trainingConfig["acceleratorType"] = config.AcceleratorType
		}
		if config.AcceleratorCount != nil && *config.AcceleratorCount != 0 {
			trainingConfig["acceleratorCount"] = *config.AcceleratorCount
		}
	}
	if config.DisplayName != "" {
		payload["displayName"] = config.DisplayName
	}
	if config.HotLoadDeploymentID != "" {
		payload["hotLoadDeploymentId"] = config.HotLoadDeploymentID
	}
	if config.Region != "" {
		trainingConfig["region"] = config.Region
	}
	if len(config.ExtraArgs) > 0 {
		trainingConfig["extraArgs"] = flattenExtraArgs(config.ExtraArgs)
	}
	if config.ForwardOnly {
		payload["forwardOnly"] = true
	}
	if config.Purpose != "" {
		payload["purpose"] = config.Purpose
	}
	if config.ManagedBy != "" {
		payload["managedBy"] = config.ManagedBy
	}
	if config.InactivityTimeout != nil {
		if value, err := FormatProtoDuration(config.InactivityTimeout); err == nil {
			payload["inactivityTimeout"] = value
		}
	}
	if config.DisableInactivityCleanup {
		payload["disableInactivityCleanup"] = true
	}
	return payload
}

func shapeOwnedFieldError(field string) string {
	return field + " cannot be set when training_shape_ref is provided. Remove it to use the shape's value, or remove training_shape_ref for a manual launch."
}

func flattenExtraArgs(args []string) []string {
	var out []string
	for _, arg := range args {
		if strings.Contains(arg, " ") {
			out = append(out, strings.Fields(arg)...)
		} else {
			out = append(out, arg)
		}
	}
	return out
}

func lastPathSegment(value string) string {
	parts := strings.Split(strings.TrimRight(value, "/"), "/")
	return parts[len(parts)-1]
}
