package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

const DefaultDecisionContract = "systemone-letter-v1"

type QuestionType string

const (
	QuestionTypeNoul   QuestionType = "noul"
	QuestionTypeChoice QuestionType = "choice"
	QuestionTypeScore  QuestionType = "score"
)

type DecisionCalibration struct {
	Contract    string                        `json:"contract"`
	Objective   string                        `json:"objective"`
	Temperature map[QuestionType]float64      `json:"temperature"`
	FitRows     map[QuestionType]int          `json:"fit_rows"`
	Metrics     map[string]map[string]float64 `json:"metrics"`
}

func (d DecisionCalibration) Validate() error {
	if d.Contract == "" {
		d.Contract = DefaultDecisionContract
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`).MatchString(d.Contract) {
		return fmt.Errorf("invalid decision contract")
	}
	if d.Objective != "" && d.Objective != "nll" {
		return fmt.Errorf("decision objective must be nll")
	}
	if d.Temperature == nil {
		return fmt.Errorf("decision temperature is required")
	}
	valid := func(q QuestionType) bool {
		return q == QuestionTypeNoul || q == QuestionTypeChoice || q == QuestionTypeScore
	}
	for q, value := range d.Temperature {
		if !valid(q) || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 100 {
			return fmt.Errorf("invalid calibration temperature for %q", q)
		}
	}
	for q, count := range d.FitRows {
		if !valid(q) || count < 0 {
			return fmt.Errorf("invalid fit_rows for %q", q)
		}
	}
	if len(d.Metrics) > 16 {
		return fmt.Errorf("calibration metrics allow at most 16 groups")
	}
	for group, values := range d.Metrics {
		if len(group) < 1 || len(group) > 64 || len(values) > 32 {
			return fmt.Errorf("invalid calibration metric group")
		}
		for key, value := range values {
			if len(key) < 1 || len(key) > 64 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("invalid calibration metric")
			}
		}
	}
	return nil
}

func (d DecisionCalibration) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.Contract == "" {
		d.Contract = DefaultDecisionContract
	}
	if d.Objective == "" {
		d.Objective = "nll"
	}
	if d.FitRows == nil {
		d.FitRows = map[QuestionType]int{}
	}
	if d.Metrics == nil {
		d.Metrics = map[string]map[string]float64{}
	}
	type wire DecisionCalibration
	return json.Marshal(wire(d))
}

type SamplerCheckpointMetadata struct {
	DecisionCalibration *DecisionCalibration `json:"decision_calibration,omitempty"`
}

func (c *FiretitanTrainingClient) ValidateLossCapabilities(loss string, config map[string]any) error {
	if config["report_policy_diagnostics"] == true && !c.SupportsPolicyDiagnostics {
		return fmt.Errorf("trainer does not advertise replay policy diagnostics")
	}
	if loss == "reinforce" {
		if !c.SupportsReinforce {
			return fmt.Errorf("trainer does not advertise built-in REINFORCE")
		}
		if config["score_centering"] == true && !c.SupportsScoreCentering {
			return fmt.Errorf("trainer does not advertise REINFORCE score centering")
		}
	} else if config["weighting"] == "tis" || config["weighting"] == "mis" || config["anchor_logprob_source"] == "current_detached" {
		if !c.SupportsPolicyImportanceSampling {
			return fmt.Errorf("trainer does not advertise server policy TIS/MIS")
		}
	}
	return nil
}

func ModelInputFromIntsWithSamplingMask(tokens []int, refs *RoutingReferences) (map[string]any, error) {
	input := ModelInputFromInts(tokens)
	if refs != nil {
		if err := refs.Validate(); err != nil {
			return nil, err
		}
		if refs.Length != len(tokens) {
			return nil, fmt.Errorf("mask references must cover every model input position")
		}
		input["sampling_mask_references"] = refs.Compact()
	}
	return input, nil
}

// Forward computes the standard forward endpoint without requesting gradients.
func (c *FiretitanTrainingClient) Forward(ctx context.Context, data []TrainingDatum, loss string, config map[string]any) (ForwardBackwardOutput, error) {
	ctx, done, err := c.beginOperation(ctx)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	defer done()
	if err := c.ValidateLossCapabilities(loss, config); err != nil {
		return ForwardBackwardOutput{}, err
	}
	support, err := TargetLogprobSupport(config)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	if support != "full_vocabulary" {
		return ForwardBackwardOutput{}, fmt.Errorf("target_logprob_support=%q requires forward_backward", support)
	}
	backend, ok := c.ComputeBackend.(TrainingForwardBackend)
	if !ok {
		return ForwardBackwardOutput{}, fmt.Errorf("compute backend does not implement forward")
	}
	normalized, err := NormalizeLossFn(loss)
	if err != nil {
		return ForwardBackwardOutput{}, err
	}
	if err := c.ValidateRoutingData(data); err != nil {
		return ForwardBackwardOutput{}, err
	}
	c.operationMu.Lock()
	defer c.operationMu.Unlock()
	c.RequestSeqID++
	return backend.Forward(ctx, ForwardBackwardOptions{Comms: c.Comms(), ModelID: c.ModelID, SeqID: c.RequestSeqID, Data: data, LossFn: normalized, LossFnConfig: cloneAnyMap(config)})
}

type samplerHandleState struct {
	InferenceURL      string            `json:"inference_url"`
	Model             string            `json:"model"`
	AdditionalHeaders map[string]string `json:"additional_headers"`
	RequestContext    map[string]any    `json:"request_context"`
	R3StoreID         *string           `json:"r3_store_id"`
	R3BindingRequired bool              `json:"r3_binding_required"`
	FixedConcurrency  *int              `json:"fixed_concurrency"`
	APIKey            *string           `json:"api_key"`
}

func credentialHeader(name string) bool {
	name = strings.ToLower(name)
	for _, marker := range []string{"authorization", "api-key", "apikey", "cf-access-client", "secret", "token", "password", "cookie"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// GetSamplerHandle exports a Python-compatible credential-free sampler handle.
func (c *FiretitanSamplingClient) GetSamplerHandle() (string, error) {
	if c == nil || c.DeploymentSampler == nil {
		return "", fmt.Errorf("sampler is required")
	}
	s := c.DeploymentSampler
	state := samplerHandleState{InferenceURL: s.InferenceURL, Model: s.Model, AdditionalHeaders: map[string]string{}, RequestContext: cleanSamplingContext(s.RequestContext)}
	for key, value := range s.AdditionalHeaders {
		if !credentialHeader(key) {
			state.AdditionalHeaders[key] = value
		}
	}
	_, store, required := s.routingBinding()
	state.R3BindingRequired = required
	if store != "" {
		state.R3StoreID = &store
	}
	if controller, ok := s.ConcurrencyController.(*FixedConcurrencyController); ok {
		limit := controller.WindowSize()
		state.FixedConcurrency = &limit
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

func FiretitanSamplingClientFromHandle(handle, apiKey string, tokenizer DeploymentTokenizer) (*FiretitanSamplingClient, error) {
	data, err := base64.URLEncoding.DecodeString(handle)
	if err != nil {
		return nil, fmt.Errorf("invalid sampler handle: %w", err)
	}
	var state samplerHandleState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	key := FireworksAPIKey(apiKey)
	if key == "" {
		return nil, fmt.Errorf("sampler handle carries no credentials; set FIREWORKS_API_KEY or provide api_key")
	}
	if state.InferenceURL == "" || state.Model == "" {
		return nil, fmt.Errorf("sampler handle requires inference_url and model")
	}
	options := []DeploymentSamplerOption{WithDeploymentSamplerAdditionalHeaders(serverlessCallerHeaders(state.AdditionalHeaders))}
	if tokenizer != nil {
		options = append(options, WithDeploymentSamplerTokenizer(tokenizer))
	}
	if state.FixedConcurrency != nil {
		if *state.FixedConcurrency < 1 {
			return nil, fmt.Errorf("invalid fixed concurrency")
		}
		options = append(options, WithDeploymentSamplerConcurrencyController(NewFixedConcurrencyController(*state.FixedConcurrency)))
	}
	sampler := NewDeploymentSampler(state.InferenceURL, state.Model, key, options...)
	if state.R3StoreID != nil {
		sampler.NegotiateRoutingMatrixFormat(RoutingParquetV1, *state.R3StoreID)
	}
	sampler.r3BindingRequired = state.R3BindingRequired
	sampler.RequestContext = cleanSamplingContext(state.RequestContext)
	return NewFiretitanSamplingClient(sampler), nil
}
