package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DeploymentReadyDefaultTimeout = 600 * time.Second
	DeploymentReadyPoll           = 15 * time.Second
	DeploymentDeletionTimeout     = 60 * time.Second
	DeploymentDeletionPoll        = 2 * time.Second
	HotloadWaitTimeout            = 400 * time.Second
	HotloadWaitPoll               = 5 * time.Second
	DefaultDeploymentDescription  = "Fireworks training deployment"
	HotloadSourceURLHeader        = "x-fireworks-hot-load-source-url"
	HotloadRecoverySteps          = "Use the Fireworks training cookbook skill's hotload recovery self-check. Common recoveries are: reattach or recreate a stale deployment; for full-parameter training, retry from a matching base checkpoint or resume from DCP; for LoRA, fix deployment attachment rather than changing checkpoint_type."
)

type DeploymentInfo struct {
	DeploymentID           string
	Name                   string
	State                  string
	HotLoadBucketURL       string
	HotLoadTrainerJob      string
	DeploymentShapeVersion string
	InferenceModel         string
}

func DeploymentHotLoadTrainerJob(deployment DeploymentInfo) string {
	return deployment.HotLoadTrainerJob
}

type DeploymentConfig struct {
	DeploymentID               string
	BaseModel                  string
	Description                string
	DeploymentShape            string
	Region                     string
	MinReplicaCount            int
	MaxReplicaCount            *int
	AcceleratorType            string
	HotLoadBucketType          *string
	HotLoadTrainerJob          string
	EnableHotLoad              *bool
	SkipShapeValidation        bool
	DisableSpeculativeDecoding bool
	ExtraArgs                  []string
	ExtraValues                map[string]string
	Annotations                map[string]string
}

type DeploymentManager struct {
	*TrainingRestClient
	InferenceURL                     string
	HotloadAPIURL                    string
	HotloadResetPromptCacheSupported *bool
	LastHotloadErrorMessage          string
	BootTime                         time.Duration
}

type DeploymentManagerOption func(*deploymentManagerConfig)

type deploymentManagerConfig struct {
	restOptions   []TrainingRestClientOption
	inferenceURL  string
	hotloadAPIURL string
}

func WithDeploymentInferenceURL(rawURL string) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.inferenceURL = rawURL
	}
}

func WithDeploymentHotloadAPIURL(rawURL string) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.hotloadAPIURL = rawURL
	}
}

func WithDeploymentAdditionalHeaders(headers map[string]string) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.restOptions = append(c.restOptions, WithTrainingAdditionalHeaders(headers))
	}
}

func WithDeploymentVerifySSL(verify bool) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.restOptions = append(c.restOptions, WithTrainingVerifySSL(verify))
	}
}

func WithDeploymentHTTPClient(httpClient *http.Client) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.restOptions = append(c.restOptions, WithTrainingHTTPClient(httpClient))
	}
}

func WithDeploymentRetryOptions(opts RequestRetryOptions) DeploymentManagerOption {
	return func(c *deploymentManagerConfig) {
		c.restOptions = append(c.restOptions, WithTrainingRetryOptions(opts))
	}
}

func NewDeploymentManager(apiKey, baseURL string, opts ...DeploymentManagerOption) *DeploymentManager {
	if baseURL == "" {
		baseURL = DefaultFireworksAPIURL
	}
	var cfg deploymentManagerConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	rest := NewTrainingRestClient(apiKey, baseURL, cfg.restOptions...)
	inferenceURL := cfg.inferenceURL
	if inferenceURL == "" {
		inferenceURL = baseURL
	}
	hotloadAPIURL := cfg.hotloadAPIURL
	if hotloadAPIURL == "" {
		hotloadAPIURL = baseURL
	}
	return &DeploymentManager{
		TrainingRestClient: rest,
		InferenceURL:       strings.TrimRight(inferenceURL, "/"),
		HotloadAPIURL:      strings.TrimRight(hotloadAPIURL, "/"),
	}
}

func (m *DeploymentManager) HotloadHeaders(ctx context.Context, deploymentID, baseModel, path string) (http.Header, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	extra := map[string]string{
		"Authorization":        "Bearer " + m.APIKey(),
		"fireworks-model":      baseModel,
		"fireworks-deployment": "accounts/" + accountID + "/deployments/" + deploymentID,
	}
	if path != "" {
		extra[HotloadSourceURLHeader] = path
	}
	return m.Headers(extra), nil
}

func (m *DeploymentManager) GetDeployment(ctx context.Context, deploymentID string) (map[string]any, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := m.Get(ctx, "/v1/accounts/"+accountID+"/deployments/"+deploymentID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get deployment %s: HTTP %d: %s", deploymentID, resp.StatusCode, ParseAPIErrorBody(body))
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *DeploymentManager) GetTrainerRegion(ctx context.Context, trainerJob string) string {
	resp, err := m.Get(ctx, "/v1/"+strings.TrimLeft(trainerJob, "/"), nil)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	trainingConfig, _ := payload["trainingConfig"].(map[string]any)
	return stringFromAny(trainingConfig["region"])
}

func (m *DeploymentManager) DeleteDeployment(ctx context.Context, deploymentID string, ignoreChecks, hard bool) error {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return err
	}
	query := url.Values{}
	if ignoreChecks {
		query.Set("ignoreChecks", "true")
	}
	if hard {
		query.Set("hard", "true")
	}
	path := "/v1/accounts/" + accountID + "/deployments/" + deploymentID
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	resp, err := m.Delete(ctx, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delete deployment %s: HTTP %d: %s", deploymentID, resp.StatusCode, ParseAPIErrorBody(body))
	}
	return nil
}

func (m *DeploymentManager) CreateDeployment(ctx context.Context, config DeploymentConfig) (map[string]any, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("deploymentId", config.DeploymentID)
	if config.SkipShapeValidation {
		query.Set("skipShapeValidation", "true")
	}
	if config.DisableSpeculativeDecoding {
		query.Set("disableSpeculativeDecoding", "true")
	}
	path := "/v1/accounts/" + accountID + "/deployments?" + query.Encode()

	region := config.Region
	if config.HotLoadTrainerJob != "" {
		trainerRegion := m.GetTrainerRegion(ctx, config.HotLoadTrainerJob)
		if trainerRegion != "" {
			if config.Region != "" && config.Region != trainerRegion {
				return nil, fmt.Errorf("hot_load_trainer_job %s is in region %s, but the deployment requests region %s; hot-load requires the deployment to be colocated with the trainer. Leave region unset to inherit the trainer's region", config.HotLoadTrainerJob, trainerRegion, config.Region)
			}
			if config.Region == "" {
				region = trainerRegion
			}
		}
	}

	resp, err := m.Post(ctx, path, BuildDeploymentBody(config, region), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		existing, getErr := m.GetDeployment(ctx, config.DeploymentID)
		if getErr != nil {
			return nil, getErr
		}
		if existing != nil {
			return existing, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		hint := HTTPStatusHints[resp.StatusCode]
		extra := ""
		if resp.StatusCode == http.StatusBadRequest {
			extra = "\n  Verify region, deployment_shape, base_model, and extra_args match the selected deployment flow.\n  For hotload, use one documented scope: PER_TRAINER via hot_load_trainer_job, or PER_DEPLOYMENT via a deployment-owned bucket."
		}
		return nil, fmt.Errorf("%s", FormatSDKError(
			fmt.Sprintf("Deployment creation failed (HTTP %d)", resp.StatusCode),
			ParseAPIErrorBody(body),
			hint+extra,
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func BuildDeploymentBody(config DeploymentConfig, resolvedRegion ...string) map[string]any {
	region := config.Region
	if len(resolvedRegion) > 0 {
		region = resolvedRegion[0]
	}
	description := config.Description
	if description == "" {
		description = DefaultDeploymentDescription
	}
	maxReplicaCount := config.MaxReplicaCount
	if maxReplicaCount == nil {
		defaultMaxReplicaCount := 1
		maxReplicaCount = &defaultMaxReplicaCount
	}
	acceleratorType := config.AcceleratorType
	if acceleratorType == "" {
		acceleratorType = "NVIDIA_H200_141GB"
	}
	hotLoadBucketType := config.HotLoadBucketType
	if hotLoadBucketType == nil {
		defaultHotLoadBucketType := "FW_HOSTED"
		hotLoadBucketType = &defaultHotLoadBucketType
	}
	enableHotLoad := true
	if config.EnableHotLoad != nil {
		enableHotLoad = *config.EnableHotLoad
	}

	body := map[string]any{
		"baseModel":       config.BaseModel,
		"description":     description,
		"minReplicaCount": config.MinReplicaCount,
		"maxReplicaCount": *maxReplicaCount,
		"enableHotLoad":   enableHotLoad,
		"forTraining":     enableHotLoad,
	}
	if region != "" {
		body["placement"] = map[string]any{"region": region}
	}
	if *hotLoadBucketType != "" {
		body["hotLoadBucketType"] = *hotLoadBucketType
	}
	if config.HotLoadTrainerJob != "" {
		body["hotLoadTrainerJob"] = config.HotLoadTrainerJob
	}
	if config.DeploymentShape != "" {
		body["deploymentShape"] = config.DeploymentShape
	} else {
		body["acceleratorType"] = acceleratorType
	}
	if len(config.ExtraArgs) > 0 {
		body["extraArgs"] = flattenExtraArgs(config.ExtraArgs)
	}
	if len(config.ExtraValues) > 0 {
		body["extraValues"] = cloneStringMap(config.ExtraValues)
	}
	if len(config.Annotations) > 0 {
		body["annotations"] = cloneStringMap(config.Annotations)
	}
	return body
}

func (m *DeploymentManager) ParseDeploymentInfo(deploymentID string, data map[string]any) DeploymentInfo {
	accountID := m.accountID
	return DeploymentInfo{
		DeploymentID:           deploymentID,
		Name:                   stringFromAny(data["name"]),
		State:                  stringOrDefault(data["state"], "UNKNOWN"),
		HotLoadBucketURL:       stringFromAny(data["hotLoadBucketUrl"]),
		HotLoadTrainerJob:      firstString(data, "hotLoadTrainerJob", "hot_load_trainer_job"),
		DeploymentShapeVersion: firstString(data, "deploymentShape", "deployment_shape"),
		InferenceModel:         "accounts/" + accountID + "/deployments/" + deploymentID,
	}
}

func (m *DeploymentManager) WaitForDeletion(ctx context.Context, deploymentID string, opts ...DeploymentWaitOptions) error {
	opt := deploymentWaitOptions(DeploymentDeletionTimeout, DeploymentDeletionPoll, opts...)
	start := opt.Now()
	for opt.Now().Sub(start) < opt.Timeout {
		data, err := m.GetDeployment(ctx, deploymentID)
		if err != nil {
			return err
		}
		if data == nil || stringFromAny(data["state"]) == "DELETED" {
			return nil
		}
		opt.Sleep(opt.PollInterval)
	}
	return nil
}

func (m *DeploymentManager) CreateOrGet(ctx context.Context, config DeploymentConfig, forceRecreate bool) (DeploymentInfo, error) {
	existing, err := m.GetDeployment(ctx, config.DeploymentID)
	if err != nil {
		return DeploymentInfo{}, err
	}
	if existing != nil {
		state := stringOrDefault(existing["state"], "UNKNOWN")
		if state == "FAILED" || state == "DELETED" || state == "DELETING" || forceRecreate {
			_ = m.DeleteDeployment(ctx, config.DeploymentID, true, true)
			_ = m.WaitForDeletion(ctx, config.DeploymentID)
		} else {
			return m.ParseDeploymentInfo(config.DeploymentID, existing), nil
		}
	}
	created, err := m.CreateDeployment(ctx, config)
	if err != nil {
		return DeploymentInfo{}, err
	}
	return m.ParseDeploymentInfo(config.DeploymentID, created), nil
}

type DeploymentWaitOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
	Now          func() time.Time
	Sleep        func(time.Duration)
	Probe        func(context.Context, string) bool
	Get          func(context.Context, string) (map[string]any, error)
}

func (m *DeploymentManager) WaitForReady(ctx context.Context, deploymentID string, opts ...DeploymentWaitOptions) (DeploymentInfo, error) {
	opt := deploymentWaitOptions(DeploymentReadyDefaultTimeout, DeploymentReadyPoll, opts...)
	if opt.Probe == nil {
		opt.Probe = m.ProbeInference
	}
	if opt.Get == nil {
		opt.Get = m.GetDeployment
	}
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return DeploymentInfo{}, err
	}
	model := "accounts/" + accountID + "/deployments/" + deploymentID
	start := opt.Now()
	for opt.Now().Sub(start) < opt.Timeout {
		data, err := opt.Get(ctx, deploymentID)
		if err != nil {
			return DeploymentInfo{}, err
		}
		if data == nil {
			return DeploymentInfo{}, fmt.Errorf("%s", FormatSDKError(
				"Deployment '"+deploymentID+"' not found",
				"The control plane returned no deployment record for this deployment ID.",
				"Verify the deployment ID and account. Create the deployment first if this is a new run.",
				SDKErrorFormatOptions{DocsURL: DocsSDK},
			))
		}
		state := stringOrDefault(data["state"], "UNKNOWN")
		if state == "READY" {
			m.BootTime = opt.Now().Sub(start)
			return m.ParseDeploymentInfo(deploymentID, data), nil
		}
		if state == "FAILED" || state == "DELETED" || state == "DELETING" {
			return DeploymentInfo{}, fmt.Errorf("%s", FormatSDKError(
				"Deployment '"+deploymentID+"' entered bad state: "+state,
				"The control plane reports deployment state "+state+", so readiness polling stopped.",
				"Check deployment events and logs in the Fireworks console: "+ConsoleURL+"\n  Recreate the deployment if the config is wrong or the resource was deleted.",
				SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
			))
		}
		if state == "CREATING" && opt.Probe(ctx, model) {
			m.BootTime = opt.Now().Sub(start)
			return m.ParseDeploymentInfo(deploymentID, data), nil
		}
		opt.Sleep(opt.PollInterval)
	}
	return DeploymentInfo{}, fmt.Errorf("%s", FormatSDKError(
		fmt.Sprintf("Deployment '%s' not ready within %.0fs", deploymentID, opt.Timeout.Seconds()),
		"The control-plane state did not reach READY and the token-in warmup probe did not return HTTP 200 before the timeout.",
		fmt.Sprintf("Increase the deployment ready timeout (current: %.0fs) and check deployment status in the Fireworks console: %s", opt.Timeout.Seconds(), ConsoleURL),
		SDKErrorFormatOptions{DocsURL: DocsSDK},
	))
}

func (m *DeploymentManager) GetInfo(ctx context.Context, deploymentID string) (DeploymentInfo, bool, error) {
	data, err := m.GetDeployment(ctx, deploymentID)
	if err != nil {
		return DeploymentInfo{}, false, err
	}
	if data == nil {
		return DeploymentInfo{}, false, nil
	}
	return m.ParseDeploymentInfo(deploymentID, data), true, nil
}

func (m *DeploymentManager) DeleteInfo(ctx context.Context, deploymentID string) error {
	return m.DeleteDeployment(ctx, deploymentID, true, true)
}

func (m *DeploymentManager) ScaleToZero(ctx context.Context, deploymentID string) error {
	return m.UpdateRaw(ctx, deploymentID, map[string]any{"maxReplicaCount": 0, "minReplicaCount": 0}, []string{"max_replica_count", "min_replica_count"})
}

func (m *DeploymentManager) UpdateRaw(ctx context.Context, deploymentID string, body map[string]any, updateMask any) error {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return err
	}
	mask := updateMaskString(updateMask)
	path := "/v1/accounts/" + accountID + "/deployments/" + deploymentID
	if mask != "" {
		query := url.Values{}
		query.Set("updateMask", mask)
		path += "?" + query.Encode()
	}
	resp, err := m.Patch(ctx, path, body, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("update deployment %s: HTTP %d: %s", deploymentID, resp.StatusCode, ParseAPIErrorBody(payload))
	}
	return nil
}

func (m *DeploymentManager) Update(ctx context.Context, deploymentID string, body map[string]any, updateMask any) (DeploymentInfo, error) {
	accountID, err := m.AccountID(ctx)
	if err != nil {
		return DeploymentInfo{}, err
	}
	mask := updateMaskString(updateMask)
	path := "/v1/accounts/" + accountID + "/deployments/" + deploymentID
	if mask != "" {
		query := url.Values{}
		query.Set("updateMask", mask)
		path += "?" + query.Encode()
	}
	resp, err := m.Patch(ctx, path, body, nil)
	if err != nil {
		return DeploymentInfo{}, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeploymentInfo{}, fmt.Errorf("update deployment %s: HTTP %d: %s", deploymentID, resp.StatusCode, ParseAPIErrorBody(payload))
	}
	var out map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &out); err != nil {
			return DeploymentInfo{}, err
		}
	}
	return m.ParseDeploymentInfo(deploymentID, out), nil
}

type HotloadOptions struct {
	IncrementalSnapshotMetadata map[string]any
	ResetPromptCache            *bool
	Timeout                     time.Duration
	Path                        string
}

func (m *DeploymentManager) Hotload(ctx context.Context, deploymentID, baseModel, snapshotIdentity string, opts ...HotloadOptions) (map[string]any, error) {
	var opt HotloadOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = HTTPWriteTimeout
	}
	resetPromptCache := true
	if opt.ResetPromptCache != nil {
		resetPromptCache = *opt.ResetPromptCache
	}
	headers, err := m.HotloadHeaders(ctx, deploymentID, baseModel, opt.Path)
	if err != nil {
		return nil, err
	}
	url := m.HotloadAPIURL + "/hot_load/v1/models/hot_load"
	includeResetPromptCache := m.HotloadResetPromptCacheSupported == nil || *m.HotloadResetPromptCacheSupported

	payload := func(includeReset bool) map[string]any {
		body := map[string]any{"identity": snapshotIdentity}
		if includeReset {
			body["reset_prompt_cache"] = resetPromptCache
		}
		if len(opt.IncrementalSnapshotMetadata) > 0 {
			body["incremental_snapshot_metadata"] = cloneAnyMap(opt.IncrementalSnapshotMetadata)
		}
		return body
	}

	resp, err := m.gatewayJSON(ctx, http.MethodPost, url, headers, payload(includeResetPromptCache), timeout)
	if err != nil {
		return nil, err
	}
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		if includeResetPromptCache && resetPromptCacheUnsupported(resp.statusCode, resp.body) {
			supported := false
			m.HotloadResetPromptCacheSupported = &supported
			resp, err = m.gatewayJSON(ctx, http.MethodPost, url, headers, payload(false), timeout)
			if err != nil {
				return nil, err
			}
		}
	} else if includeResetPromptCache {
		supported := true
		m.HotloadResetPromptCacheSupported = &supported
	}
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		hint := HTTPStatusHints[resp.statusCode]
		return nil, fmt.Errorf("%s", FormatSDKError(
			fmt.Sprintf("Hotload API error (HTTP %d)", resp.statusCode),
			ParseAPIErrorBody(resp.body),
			hint+"\n  Verify the deployment is hotload-enabled, the base model matches the deployment, and the snapshot identity came from save_weights_for_sampler.",
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	return decodeJSONMapOrEmpty(resp.body)
}

func (m *DeploymentManager) HotloadCheckStatus(ctx context.Context, deploymentID, baseModel string, timeout ...time.Duration) (map[string]any, error) {
	requestTimeout := HTTPReadTimeout
	if len(timeout) > 0 && timeout[0] != 0 {
		requestTimeout = timeout[0]
	}
	headers, err := m.HotloadHeaders(ctx, deploymentID, baseModel, "")
	if err != nil {
		return nil, err
	}
	resp, err := m.gatewayJSON(ctx, http.MethodGet, m.HotloadAPIURL+"/hot_load/v1/models/hot_load", headers, nil, requestTimeout)
	if err != nil {
		return nil, err
	}
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		return nil, fmt.Errorf("hotload status: HTTP %d: %s", resp.statusCode, ParseAPIErrorBody(resp.body))
	}
	return decodeJSONMapOrEmpty(resp.body)
}

type HotloadWaitOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
	Now          func() time.Time
	Sleep        func(time.Duration)
	CheckStatus  func(context.Context, string, string) (map[string]any, error)
}

func (m *DeploymentManager) WaitForHotload(ctx context.Context, deploymentID, baseModel, expectedIdentity string, opts ...HotloadWaitOptions) (bool, error) {
	opt := hotloadWaitOptions(opts...)
	if opt.CheckStatus == nil {
		opt.CheckStatus = func(ctx context.Context, deploymentID, baseModel string) (map[string]any, error) {
			return m.HotloadCheckStatus(ctx, deploymentID, baseModel)
		}
	}
	start := opt.Now()
	m.LastHotloadErrorMessage = ""
	var lastCurrentIdentity string
	lastStage := "unknown"
	var lastReadiness *bool

	for opt.Now().Sub(start) < opt.Timeout {
		status, err := opt.CheckStatus(ctx, deploymentID, baseModel)
		if err != nil {
			opt.Sleep(opt.PollInterval)
			continue
		}
		replicas, ok := status["replicas"].([]any)
		if !ok {
			keys := make([]string, 0, len(status))
			for key := range status {
				keys = append(keys, key)
			}
			return false, fmt.Errorf("%s", FormatSDKError(
				"Unrecognized hotload status response format",
				fmt.Sprintf("Expected 'replicas' list, got keys: %v", keys),
				"The SDK hotload waiter expects the serving status endpoint to return a replicas list. Check the cookbook skill for the supported SDK/serving path, then retry with matching versions.",
				SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
			))
		}

		var replica map[string]any
		var loadedAdapters []any
		currentIdentity := ""
		stage := "pending"
		readiness := false
		if len(replicas) > 0 {
			replica, _ = replicas[0].(map[string]any)
			if replica == nil {
				replica = map[string]any{}
			}
			currentIdentity = stringFromAny(replica["current_snapshot_identity"])
			loadingState, _ := replica["loading_state"].(map[string]any)
			stage = stringOrDefault(loadingState["stage"], "unknown")
			readiness = truthy(replica["readiness"])
			loadedAdapters, _ = replica["loaded_adapters"].([]any)
		}
		lastCurrentIdentity = currentIdentity
		lastStage = stage
		lastReadiness = &readiness

		if readiness && (currentIdentity == expectedIdentity || loadedAdapterReady(loadedAdapters, expectedIdentity)) {
			return true, nil
		}
		if stage == "error" {
			loadingState, _ := replica["loading_state"].(map[string]any)
			errorTarget := stringFromAny(loadingState["target_snapshot_identity"])
			if errorTarget == expectedIdentity {
				cause := "The deployment status reported an error for the requested snapshot. Expected client snapshot: " + expectedIdentity + "; current deployment snapshot: " + formatSnapshotIdentity(currentIdentity) + "; target snapshot: " + errorTarget + "; stage=error."
				m.LastHotloadErrorMessage = FormatSDKError(
					"Hotload failed for snapshot '"+expectedIdentity+"'",
					cause,
					HotloadRecoverySteps,
					SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
				)
				return false, nil
			}
		}
		opt.Sleep(opt.PollInterval)
	}
	m.LastHotloadErrorMessage = m.formatHotloadTimeoutError(expectedIdentity, opt.Timeout, lastCurrentIdentity, lastStage, lastReadiness)
	return false, nil
}

func (m *DeploymentManager) HotloadAndWait(ctx context.Context, deploymentID, baseModel, snapshotIdentity string, opts ...HotloadAndWaitOptions) (bool, error) {
	var opt HotloadAndWaitOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	_, err := m.Hotload(ctx, deploymentID, baseModel, snapshotIdentity, HotloadOptions{
		IncrementalSnapshotMetadata: opt.IncrementalSnapshotMetadata,
		ResetPromptCache:            opt.ResetPromptCache,
		Timeout:                     opt.RequestTimeout,
		Path:                        opt.Path,
	})
	if err != nil {
		return false, err
	}
	return m.WaitForHotload(ctx, deploymentID, baseModel, snapshotIdentity, opt.Wait)
}

type HotloadAndWaitOptions struct {
	IncrementalSnapshotMetadata map[string]any
	ResetPromptCache            *bool
	RequestTimeout              time.Duration
	Path                        string
	Wait                        HotloadWaitOptions
}

type ReattachTrainerOptions struct {
	Timeout             time.Duration
	PollInterval        time.Duration
	Now                 func() time.Time
	Sleep               func(time.Duration)
	ReadReplicaIdentity func(context.Context, string, string) (string, error)
	Update              func(context.Context, string, map[string]any, any) (DeploymentInfo, error)
}

func (m *DeploymentManager) ReattachTrainer(ctx context.Context, deployment DeploymentInfo, baseModel, trainerJobName string, opts ...ReattachTrainerOptions) (DeploymentInfo, error) {
	if DeploymentHotLoadTrainerJob(deployment) == trainerJobName {
		return deployment, nil
	}
	return m.reattachTrainer(ctx, deployment.DeploymentID, baseModel, trainerJobName, opts...)
}

func (m *DeploymentManager) ReattachTrainerByID(ctx context.Context, deploymentID, baseModel, trainerJobName string, opts ...ReattachTrainerOptions) (DeploymentInfo, error) {
	deployment, ok, err := m.GetInfo(ctx, deploymentID)
	if err != nil {
		return DeploymentInfo{}, err
	}
	if !ok {
		return DeploymentInfo{}, fmt.Errorf("deployment %q does not exist", deploymentID)
	}
	return m.ReattachTrainer(ctx, deployment, baseModel, trainerJobName, opts...)
}

func (m *DeploymentManager) reattachTrainer(ctx context.Context, deploymentID, baseModel, trainerJobName string, opts ...ReattachTrainerOptions) (DeploymentInfo, error) {
	opt := reattachTrainerOptions(opts...)
	if opt.ReadReplicaIdentity == nil {
		opt.ReadReplicaIdentity = m.ReadReplicaIdentity
	}
	if opt.Update == nil {
		opt.Update = m.Update
	}
	prevIdentity, _ := opt.ReadReplicaIdentity(ctx, deploymentID, baseModel)
	updated, err := opt.Update(ctx, deploymentID, map[string]any{"hotLoadTrainerJob": trainerJobName}, "hot_load_trainer_job")
	if err != nil {
		return DeploymentInfo{}, err
	}
	deadline := opt.Now().Add(maxDuration(opt.Timeout, time.Second))
	sawPodGone := prevIdentity == ""
	for opt.Now().Before(deadline) {
		current, _ := opt.ReadReplicaIdentity(ctx, deploymentID, baseModel)
		if prevIdentity == "" {
			if current != "" {
				return updated, nil
			}
		} else if current == "" {
			sawPodGone = true
		} else if sawPodGone && current != prevIdentity {
			return updated, nil
		} else if current != prevIdentity {
			return updated, nil
		}
		opt.Sleep(opt.PollInterval)
	}
	return DeploymentInfo{}, fmt.Errorf("re-attach for deployment %q did not produce a fresh pod within %.0fs (prev_identity=%q)", deploymentID, opt.Timeout.Seconds(), prevIdentity)
}

func (m *DeploymentManager) ReadReplicaIdentity(ctx context.Context, deploymentID, baseModel string) (string, error) {
	status, err := m.HotloadCheckStatus(ctx, deploymentID, baseModel)
	if err != nil {
		return "", err
	}
	replicas, _ := status["replicas"].([]any)
	if len(replicas) == 0 {
		return "", nil
	}
	replica, _ := replicas[0].(map[string]any)
	if replica == nil {
		return "", nil
	}
	if identity := stringFromAny(replica["current_snapshot_identity"]); identity != "" {
		return identity, nil
	}
	return stringFromAny(replica["identity"]), nil
}

type WarmupOptions struct {
	MaxRetries    int
	RetryInterval time.Duration
	Sleep         func(time.Duration)
}

func (m *DeploymentManager) Warmup(ctx context.Context, model string, opts ...WarmupOptions) bool {
	opt := WarmupOptions{
		MaxRetries:    30,
		RetryInterval: 10 * time.Second,
		Sleep:         time.Sleep,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.MaxRetries != 0 {
			opt.MaxRetries = provided.MaxRetries
		}
		if provided.RetryInterval != 0 {
			opt.RetryInterval = provided.RetryInterval
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
	}
	for attempt := 0; attempt < opt.MaxRetries; attempt++ {
		if m.ProbeInference(ctx, model) {
			return true
		}
		opt.Sleep(opt.RetryInterval)
	}
	return false
}

func (m *DeploymentManager) ProbeInference(ctx context.Context, model string) bool {
	payload := map[string]any{
		"model":       model,
		"prompt":      []int{1, 2},
		"max_tokens":  4,
		"temperature": 0.0,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.InferenceURL+"/inference/v1/completions", bytes.NewReader(data))
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

func deploymentWaitOptions(defaultTimeout, defaultPoll time.Duration, opts ...DeploymentWaitOptions) DeploymentWaitOptions {
	opt := DeploymentWaitOptions{
		Timeout:      defaultTimeout,
		PollInterval: defaultPoll,
		Now:          time.Now,
		Sleep:        time.Sleep,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.Timeout != 0 {
			opt.Timeout = provided.Timeout
		}
		if provided.PollInterval != 0 {
			opt.PollInterval = provided.PollInterval
		}
		if provided.Now != nil {
			opt.Now = provided.Now
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
		if provided.Probe != nil {
			opt.Probe = provided.Probe
		}
		if provided.Get != nil {
			opt.Get = provided.Get
		}
	}
	return opt
}

func firstString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(data[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringOrDefault(value any, fallback string) string {
	if got := stringFromAny(value); got != "" {
		return got
	}
	return fallback
}

func updateMaskString(updateMask any) string {
	switch value := updateMask.(type) {
	case string:
		return value
	case []string:
		return strings.Join(value, ",")
	default:
		return ""
	}
}

type gatewayJSONResponse struct {
	statusCode int
	body       []byte
}

func (m *DeploymentManager) gatewayJSON(ctx context.Context, method, rawURL string, headers http.Header, body any, timeout time.Duration) (gatewayJSONResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return gatewayJSONResponse{}, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return gatewayJSONResponse{}, err
	}
	req.Header = headers.Clone()
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return gatewayJSONResponse{}, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return gatewayJSONResponse{statusCode: resp.StatusCode, body: payload}, nil
}

func resetPromptCacheUnsupported(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(ParseAPIErrorBody(body) + " " + strings.TrimSpace(string(body)))
	return strings.Contains(message, "reset_prompt_cache") && strings.Contains(message, "extra inputs are not permitted")
}

func decodeJSONMapOrEmpty(body []byte) (map[string]any, error) {
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func hotloadWaitOptions(opts ...HotloadWaitOptions) HotloadWaitOptions {
	opt := HotloadWaitOptions{
		Timeout:      HotloadWaitTimeout,
		PollInterval: HotloadWaitPoll,
		Now:          time.Now,
		Sleep:        time.Sleep,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.Timeout != 0 {
			opt.Timeout = provided.Timeout
		}
		if provided.PollInterval != 0 {
			opt.PollInterval = provided.PollInterval
		}
		if provided.Now != nil {
			opt.Now = provided.Now
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
		if provided.CheckStatus != nil {
			opt.CheckStatus = provided.CheckStatus
		}
	}
	return opt
}

func loadedAdapterReady(adapters []any, expectedIdentity string) bool {
	for _, adapter := range adapters {
		row, _ := adapter.(map[string]any)
		if row == nil {
			continue
		}
		if stringFromAny(row["identity"]) == expectedIdentity && stringFromAny(row["status"]) == "loaded" {
			return true
		}
	}
	return false
}

func (m *DeploymentManager) formatHotloadTimeoutError(expectedIdentity string, timeout time.Duration, currentIdentity, stage string, readiness *bool) string {
	snapshotState := "Expected client snapshot: " + expectedIdentity + "; current deployment snapshot: " + formatSnapshotIdentity(currentIdentity) + "."
	var cause string
	if currentIdentity != "" && currentIdentity != expectedIdentity {
		cause = "The deployment status did not report the requested snapshot as loaded before the timeout. " + snapshotState
	} else {
		cause = "The deployment status did not become ready for the requested snapshot before the timeout. " + snapshotState
	}
	if stage != "unknown" || readiness != nil {
		ready := "unknown"
		if readiness != nil {
			ready = fmt.Sprintf("%t", *readiness)
		}
		cause += " Last hotload state: stage=" + stage + ", ready=" + ready + "."
	}
	return FormatSDKError(
		fmt.Sprintf("Hotload did not complete within %.0fs", timeout.Seconds()),
		cause,
		fmt.Sprintf("%s\n  If the deployment is simply slow or unhealthy, increase the hotload timeout (current: %.0fs) and check deployment health in the Fireworks console: %s", HotloadRecoverySteps, timeout.Seconds(), ConsoleURL),
		SDKErrorFormatOptions{DocsURL: DocsSDK},
	)
}

func formatSnapshotIdentity(identity string) string {
	if identity == "" {
		return "none"
	}
	return identity
}

func reattachTrainerOptions(opts ...ReattachTrainerOptions) ReattachTrainerOptions {
	opt := ReattachTrainerOptions{
		Timeout:      ReattachSettleTimeout,
		PollInterval: HotloadWaitPoll,
		Now:          time.Now,
		Sleep:        time.Sleep,
	}
	if len(opts) > 0 {
		provided := opts[0]
		if provided.Timeout != 0 {
			opt.Timeout = provided.Timeout
		}
		if provided.PollInterval != 0 {
			opt.PollInterval = provided.PollInterval
		}
		if provided.Now != nil {
			opt.Now = provided.Now
		}
		if provided.Sleep != nil {
			opt.Sleep = provided.Sleep
		}
		if provided.ReadReplicaIdentity != nil {
			opt.ReadReplicaIdentity = provided.ReadReplicaIdentity
		}
		if provided.Update != nil {
			opt.Update = provided.Update
		}
	}
	return opt
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
