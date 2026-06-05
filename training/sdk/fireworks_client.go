package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const pollTransientMaxBackoff = 60 * time.Second

var (
	resourceIDRE     = regexp.MustCompile(`^[a-z0-9-]+$`)
	checkpointNameRE = regexp.MustCompile(`^accounts/([^/]+)/rlorTrainerJobs/([^/]+)/checkpoints/([^/]+)$`)
	trainingShapeRE  = regexp.MustCompile(`^accounts/[^/]+/trainingShapes/[^/]+(/versions/[^/]+)?$`)
)

type TransientOperationPollError struct {
	Message string
	Err     error
}

func (e *TransientOperationPollError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "transient operation poll error"
}

func (e *TransientOperationPollError) Unwrap() error {
	return e.Err
}

type FireworksClient struct {
	*TrainingRestClient
}

type TrainingShapeProfile struct {
	TrainingShapeVersion      string
	TrainerImageTag           string
	MaxSupportedContextLength int
	NodeCount                 int
	DeploymentShapeVersion    string
	DeploymentImageTag        string
	AcceleratorType           string
	AcceleratorCount          int
	BaseModelWeightPrecision  string
	PipelineParallelism       int
	TrainerMode               string
}

func (p TrainingShapeProfile) TrainingShape() string {
	if p.TrainingShapeVersion == "" {
		return ""
	}
	idx := strings.Index(p.TrainingShapeVersion, "/versions/")
	if idx == -1 {
		return p.TrainingShapeVersion
	}
	return p.TrainingShapeVersion[:idx]
}

func (p TrainingShapeProfile) DeploymentShape() string {
	return p.DeploymentShapeVersion
}

func (p TrainingShapeProfile) SupportsLora() bool {
	return p.TrainerMode == "LORA_TRAINER"
}

func NewFireworksClient(apiKey, baseURL string, opts ...TrainingRestClientOption) *FireworksClient {
	return &FireworksClient{TrainingRestClient: NewTrainingRestClient(apiKey, baseURL, opts...)}
}

func ValidateOutputModelID(outputModelID string) []string {
	var errors []string
	if outputModelID == "" {
		errors = append(errors, "output_model_id is required")
	} else if !resourceIDRE.MatchString(outputModelID) {
		errors = append(errors, fmt.Sprintf("output_model_id %q contains invalid characters. Must be lowercase a-z, 0-9, or hyphen.", outputModelID))
	}
	if outputModelID != "" && len(outputModelID) > 63 {
		errors = append(errors, fmt.Sprintf("output_model_id %q is too long (%d chars). Maximum is 63 characters.", outputModelID, len(outputModelID)))
	}
	return errors
}

func ParseCheckpointName(name string) (accountID, jobID, checkpointID string, ok bool) {
	matches := checkpointNameRE.FindStringSubmatch(name)
	if matches == nil {
		return "", "", "", false
	}
	return matches[1], matches[2], matches[3], true
}

func (c *FireworksClient) ResolveTrainingProfile(ctx context.Context, trainingShapeID string) (TrainingShapeProfile, error) {
	isVersioned := strings.Contains(trainingShapeID, "/versions/")
	if !trainingShapeRE.MatchString(trainingShapeID) {
		return TrainingShapeProfile{}, fmt.Errorf("%s", FormatSDKError(
			"Invalid training_shape_id format",
			fmt.Sprintf("%q is not a valid training shape resource name.", trainingShapeID),
			"Expected: accounts/<account>/trainingShapes/<shape>[/versions/<ver>]\n  Example: accounts/fireworks/trainingShapes/ts-qwen3-8b-policy",
		))
	}

	path := "/v1/" + trainingShapeID
	if !isVersioned {
		query := url.Values{}
		query.Set("filter", "latest_validated=true")
		query.Set("pageSize", "1")
		path += "/versions?" + query.Encode()
	}
	resp, err := c.Get(ctx, path, nil)
	if err != nil {
		return TrainingShapeProfile{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TrainingShapeProfile{}, trainingShapeError(resp.StatusCode, body, trainingShapeID)
	}
	var data map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &data); err != nil {
			return TrainingShapeProfile{}, err
		}
	}
	version, err := SelectTrainingShapeVersion(data, isVersioned, trainingShapeID)
	if err != nil {
		return TrainingShapeProfile{}, err
	}
	return ProfileFromTrainingShapeVersion(version), nil
}

func SelectTrainingShapeVersion(data map[string]any, isVersioned bool, trainingShapeID string) (map[string]any, error) {
	if isVersioned {
		return data, nil
	}
	versions, _ := data["trainingShapeVersions"].([]any)
	if len(versions) == 0 {
		return nil, fmt.Errorf("%s", FormatSDKError(
			"Failed to resolve latest validated training shape for '"+trainingShapeID+"'",
			"No latest validated training-shape version was returned.",
			"Pass a versioned training_shape_id, or validate one version of this shape before using the unversioned shape resource.",
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	version, _ := versions[0].(map[string]any)
	if version == nil {
		return nil, fmt.Errorf("training shape version payload was not an object")
	}
	return version, nil
}

func ProfileFromTrainingShapeVersion(version map[string]any) TrainingShapeProfile {
	snapshot, _ := version["snapshot"].(map[string]any)
	sharding, _ := snapshot["trainerShardingScheme"].(map[string]any)
	pp := intFromAny(sharding["pipelineParallelism"], 1)
	return TrainingShapeProfile{
		TrainingShapeVersion:      stringFromAny(version["name"]),
		TrainerImageTag:           stringFromAny(snapshot["trainerImageTag"]),
		MaxSupportedContextLength: intFromAny(snapshot["maxSupportedContextLength"], 0),
		NodeCount:                 intFromAny(snapshot["nodeCount"], 1),
		DeploymentShapeVersion:    stringFromAny(snapshot["deploymentShapeVersion"]),
		DeploymentImageTag:        stringFromAny(snapshot["deploymentImageTag"]),
		AcceleratorType:           stringFromAny(snapshot["acceleratorType"]),
		AcceleratorCount:          intFromAny(snapshot["acceleratorCount"], 0),
		BaseModelWeightPrecision:  stringFromAny(snapshot["baseModelWeightPrecision"]),
		PipelineParallelism:       pp,
		TrainerMode:               stringFromAny(snapshot["trainerMode"]),
	}
}

func (c *FireworksClient) GetOperation(ctx context.Context, name string) (map[string]any, error) {
	resp, err := c.Get(ctx, "/v1/"+name, nil)
	if err != nil {
		return nil, &TransientOperationPollError{Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return decodeJSONMap(resp.Body)
	}
	body, _ := io.ReadAll(resp.Body)
	message := ParseAPIErrorBody(body)
	if IsRetryableStatusCode(resp.StatusCode) {
		return nil, &TransientOperationPollError{Message: message}
	}
	return nil, fmt.Errorf("%s", FormatSDKError(
		"Failed to poll operation '"+name+"'",
		message,
		"Operation polling retries transient HTTP and network failures automatically. Retry the original request; if this persists, inspect the operation in the Fireworks console.",
		SDKErrorFormatOptions{DocsURL: DocsSDK},
	))
}

type OperationWaitOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
	Now          func() time.Time
	Sleep        func(time.Duration)
}

func (c *FireworksClient) WaitForOperation(ctx context.Context, operation map[string]any, opts ...OperationWaitOptions) (map[string]any, error) {
	var opt OperationWaitOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 7200 * time.Second
	}
	if opt.PollInterval < 0 {
		opt.PollInterval = 0
	} else if opt.PollInterval == 0 && len(opts) == 0 {
		opt.PollInterval = 10 * time.Second
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Sleep == nil {
		opt.Sleep = time.Sleep
	}

	name, _ := operation["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("Operation response did not include a name")
	}
	deadline := opt.Now().Add(opt.Timeout)
	current := operation
	consecutiveTransientErrors := 0
	for !truthy(current["done"]) {
		if !opt.Now().Before(deadline) {
			return nil, fmt.Errorf("Operation %q did not complete within %.0fs", name, opt.Timeout.Seconds())
		}
		opt.Sleep(opt.PollInterval)
		next, err := c.GetOperation(ctx, name)
		if err != nil {
			var transient *TransientOperationPollError
			if !errors.As(err, &transient) {
				return nil, err
			}
			consecutiveTransientErrors++
			backoff := opt.PollInterval * time.Duration(1<<minInt(consecutiveTransientErrors-1, 5))
			if backoff > pollTransientMaxBackoff {
				backoff = pollTransientMaxBackoff
			}
			remaining := time.Until(deadline)
			if opt.Now != nil {
				remaining = deadline.Sub(opt.Now())
			}
			if remaining <= 0 {
				return nil, fmt.Errorf("Operation %q did not complete within %.0fs", name, opt.Timeout.Seconds())
			}
			if backoff > remaining {
				backoff = remaining
			}
			opt.Sleep(backoff)
			continue
		}
		consecutiveTransientErrors = 0
		current = next
	}
	if errPayload, ok := current["error"]; ok && errPayload != nil {
		message := fmt.Sprint(errPayload)
		if errMap, ok := errPayload.(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok {
				message = msg
			}
		}
		return nil, fmt.Errorf("%s", FormatSDKError(
			"Operation '"+name+"' failed",
			message,
			"The control-plane operation reached done=true with an error payload. Use the operation message above and the Fireworks console to decide whether to retry.",
			SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: true},
		))
	}
	return current, nil
}

type PromoteCheckpointOptions struct {
	JobID               string
	CheckpointID        string
	OutputModelID       string
	BaseModel           string
	Name                string
	HotLoadDeploymentID string
	WaitOptions         OperationWaitOptions
}

func (c *FireworksClient) PromoteCheckpoint(ctx context.Context, opts PromoteCheckpointOptions) (map[string]any, error) {
	accountID, jobID, checkpointID, err := c.ResolvePromoteTarget(ctx, opts.Name, opts.JobID, opts.CheckpointID)
	if err != nil {
		return nil, err
	}
	if opts.OutputModelID == "" || opts.BaseModel == "" {
		return nil, fmt.Errorf("output_model_id and base_model are required")
	}
	if errors := ValidateOutputModelID(opts.OutputModelID); len(errors) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errors, "\n\n"))
	}

	path, body := BuildPromoteRequest(accountID, jobID, checkpointID, opts.OutputModelID, opts.BaseModel, opts.HotLoadDeploymentID)
	bodyWithAsync := cloneAnyMap(body)
	bodyWithAsync["async_promotion"] = true
	resp, err := c.Post(ctx, path, bodyWithAsync, nil)
	if err != nil {
		return nil, err
	}
	result, retry, err := c.decodePromoteResponse(resp)
	if err != nil {
		return nil, err
	}
	if retry {
		resp, err = c.Post(ctx, path, body, nil)
		if err != nil {
			return nil, err
		}
		result, _, err = c.decodePromoteResponse(resp)
		if err != nil {
			return nil, err
		}
	}

	if operation, ok := result["operation"].(map[string]any); ok {
		operation, err = c.WaitForOperation(ctx, operation, opts.WaitOptions)
		if err != nil {
			return nil, err
		}
		model, _ := operation["response"].(map[string]any)
		if model == nil {
			model, _ = result["model"].(map[string]any)
		}
		if model == nil {
			model = c.FetchPromotedModel(ctx, accountID, opts.OutputModelID)
		}
		if model == nil {
			return nil, fmt.Errorf("%s", FormatSDKError(
				"Failed to promote checkpoint '"+checkpointID+"'",
				"promotion operation completed without a model response",
				"The promote operation finished, but neither the server response nor a follow-up model lookup returned the promoted model payload. Check the operation and output model in the Fireworks console.\n  Console: "+ConsoleURL,
				SDKErrorFormatOptions{DocsURL: DocsSDK},
			))
		}
		delete(model, "@type")
		return model, nil
	}
	if model, ok := result["model"].(map[string]any); ok {
		return model, nil
	}
	return result, nil
}

func (c *FireworksClient) ListCheckpoints(ctx context.Context, jobID string, pageSize int) ([]map[string]any, error) {
	if pageSize <= 0 {
		pageSize = 200
	}
	accountID, err := c.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	basePath := "/v1/accounts/" + accountID + "/rlorTrainerJobs/" + jobID + "/checkpoints"
	var rows []map[string]any
	pageToken := ""
	for {
		query := url.Values{}
		query.Set("pageSize", fmt.Sprint(pageSize))
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		resp, err := c.Get(ctx, basePath+"?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, listCheckpointsError(resp.StatusCode, body, jobID)
		}
		var payload map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				return nil, err
			}
		}
		for _, item := range checkpointPage(payload) {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
		pageToken = stringFromAny(payload["nextPageToken"])
		if pageToken == "" {
			pageToken = stringFromAny(payload["next_page_token"])
		}
		if pageToken == "" {
			break
		}
	}
	return rows, nil
}

func (c *FireworksClient) ResolvePromoteTarget(ctx context.Context, name, jobID, checkpointID string) (string, string, string, error) {
	if name != "" {
		if checkpointID != "" {
			return "", "", "", fmt.Errorf("pass either name= or checkpoint_id, not both")
		}
		accountID, parsedJobID, parsedCheckpointID, ok := ParseCheckpointName(name)
		if !ok {
			return "", "", "", fmt.Errorf("invalid checkpoint name %q. Expected 4 segments: accounts/<account>/rlorTrainerJobs/<job>/checkpoints/<id>", name)
		}
		if jobID != "" && jobID != parsedJobID {
			return "", "", "", fmt.Errorf("job_id=%q conflicts with name's trainer job %q", jobID, parsedJobID)
		}
		return accountID, parsedJobID, parsedCheckpointID, nil
	}
	if jobID == "" || checkpointID == "" {
		return "", "", "", fmt.Errorf("Either name= (4-segment resource path) or both job_id and checkpoint_id are required")
	}
	accountID, err := c.AccountID(ctx)
	if err != nil {
		return "", "", "", err
	}
	return accountID, jobID, checkpointID, nil
}

func BuildPromoteRequest(accountID, jobID, checkpointID, outputModelID, baseModel, hotLoadDeploymentID string) (string, map[string]any) {
	path := "/v1/accounts/" + accountID + "/checkpoints/" + checkpointID + ":promote"
	body := map[string]any{
		"output_model":   "accounts/" + accountID + "/models/" + outputModelID,
		"trainer_job_id": "accounts/" + accountID + "/rlorTrainerJobs/" + jobID,
		"base_model":     baseModel,
	}
	if hotLoadDeploymentID != "" {
		body["hot_load_deployment_id"] = "accounts/" + accountID + "/deployments/" + hotLoadDeploymentID
	}
	return path, body
}

func (c *FireworksClient) FetchPromotedModel(ctx context.Context, accountID, outputModelID string) map[string]any {
	resp, err := c.Get(ctx, "/v1/accounts/"+accountID+"/models/"+outputModelID, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	model, err := decodeJSONMap(resp.Body)
	if err != nil {
		return nil
	}
	return model
}

func (c *FireworksClient) decodePromoteResponse(resp *http.Response) (map[string]any, bool, error) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && isUnknownAsyncPromotionField(body) {
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("%s", FormatSDKError(
			"Failed to promote checkpoint",
			ParseAPIErrorBody(body),
			"Use a checkpoint name returned by list_checkpoints, ensure the row is promotable, and pass the base_model that matches the trainer.\n  Console: "+ConsoleURL,
			SDKErrorFormatOptions{DocsURL: DocsSDK},
		))
	}
	var result map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false, err
		}
	}
	if result == nil {
		result = make(map[string]any)
	}
	return result, false, nil
}

func isUnknownAsyncPromotionField(body []byte) bool {
	msg := strings.ToLower(ParseAPIErrorBody(body))
	return strings.Contains(msg, "async_promotion") || strings.Contains(msg, "asyncpromotion")
}

func decodeJSONMap(reader io.Reader) (map[string]any, error) {
	var out map[string]any
	if err := json.NewDecoder(reader).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func trainingShapeError(statusCode int, body []byte, trainingShapeID string) error {
	errorMsg := ParseAPIErrorBody(body)
	solution := "Verify the training_shape_id is registered and visible to the account resolved from your API key."
	showSupport := true
	switch statusCode {
	case http.StatusUnauthorized:
		solution = "The API key was rejected for training APIs. Ensure you are using a training-scoped Fireworks API key; inference-only keys return 401 here."
		showSupport = false
	case http.StatusNotFound:
		solution = "Training shape '" + trainingShapeID + "' was not found. Verify the resource name, version segment, and account visibility."
		showSupport = false
	case http.StatusForbidden:
		solution = "Permission denied for training shape '" + trainingShapeID + "'. Ensure your account owns or has access to this shape."
		showSupport = false
	}
	return fmt.Errorf("%s", FormatSDKError(
		fmt.Sprintf("Failed to fetch training shape %q (HTTP %d)", trainingShapeID, statusCode),
		errorMsg,
		solution,
		SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: showSupport},
	))
}

func listCheckpointsError(statusCode int, body []byte, jobID string) error {
	errorMsg := ParseAPIErrorBody(body)
	solution := "Retry; if it persists, contact Fireworks support."
	showSupport := true
	switch statusCode {
	case http.StatusNotFound:
		solution = "Trainer job '" + jobID + "' was not found in this account. Verify the job ID and that your API key resolves to the account that owns it. If the trainer was deleted after the retention window, the checkpoint rows and backing blobs are expected to be gone."
		showSupport = false
	case http.StatusForbidden:
		solution = "Your API key does not have access to this trainer job."
		showSupport = false
	}
	return fmt.Errorf("%s", FormatSDKError(
		fmt.Sprintf("Failed to list checkpoints for trainer job %q (HTTP %d)", jobID, statusCode),
		errorMsg,
		solution,
		SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: showSupport},
	))
}

func checkpointPage(payload map[string]any) []any {
	if payload == nil {
		return nil
	}
	if rows, ok := payload["checkpoints"].([]any); ok {
		return rows
	}
	if rows, ok := payload["rlorTrainerJobCheckpoints"].([]any); ok {
		return rows
	}
	return nil
}

func stringFromAny(value any) string {
	got, _ := value.(string)
	return got
}

func intFromAny(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return fallback
	}
}

func truthy(value any) bool {
	got, _ := value.(bool)
	return got
}

func cloneAnyMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
