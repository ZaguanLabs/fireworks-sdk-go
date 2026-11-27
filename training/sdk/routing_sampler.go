package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
)

// NegotiateRoutingMatrixFormat binds a sampler to the trainer's shared store.
func (s *DeploymentSampler) NegotiateRoutingMatrixFormat(format RoutingMatrixFormat, storeID string) {
	s.r3Mu.Lock()
	defer s.r3Mu.Unlock()
	s.routingMatrixFormat = ""
	s.r3StoreID = ""
	if format == RoutingParquetV1 && storeID != "" {
		s.r3StoreID = storeID
		s.routingMatrixFormat = RoutingParquetV1
	}
}
func (s *DeploymentSampler) routingBinding() (RoutingMatrixFormat, string, bool) {
	s.r3Mu.Lock()
	defer s.r3Mu.Unlock()
	return s.routingMatrixFormat, s.r3StoreID, s.r3BindingRequired
}
func (s *DeploymentSampler) RoutingMatrixFormat() RoutingMatrixFormat {
	f, _, _ := s.routingBinding()
	return f
}
func (s *DeploymentSampler) R3StoreID() string { _, id, _ := s.routingBinding(); return id }

// A custom transport belongs to its caller. A copied handle may send through it,
// but closing one handle must not close another handle's connections.
type borrowedSamplerTransport struct{ http.RoundTripper }

func (s *DeploymentSampler) CopyForR3Binding(format RoutingMatrixFormat, store string) *DeploymentSampler {
	clone := NewDeploymentSampler(s.InferenceURL, s.Model, s.APIKey)
	// Preserve injected requesters; bind the built-in requester to the new sampler.
	if s.CompletionRequester != nil && reflect.ValueOf(s.CompletionRequester).Pointer() != reflect.ValueOf(s.defaultCompletionRequest).Pointer() {
		clone.CompletionRequester = s.CompletionRequester
	}
	clone.Tokenizer = s.Tokenizer
	clone.ConcurrencyController = s.ConcurrencyController
	clone.backendManagedConcurrency = s.backendManagedConcurrency
	clone.Now = s.Now
	clone.Sleep = s.Sleep
	clone.RetryJitter = s.RetryJitter
	clone.AdditionalHeaders = cloneStringMap(s.AdditionalHeaders)
	clone.RequestContext = cloneAnyMap(s.RequestContext)
	if s.HTTPClient != nil {
		client := *s.HTTPClient
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		if t, ok := transport.(*http.Transport); ok {
			client.Transport = t.Clone()
		} else {
			client.Transport = borrowedSamplerTransport{transport}
		}
		clone.HTTPClient = &client
	}
	clone.NegotiateRoutingMatrixFormat(format, store)
	return clone
}
func (s *DeploymentSampler) completionHeaders(opts CompletionRequestOptions) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("X-Api-Key", s.APIKey)
	h.Set("Authorization", "Bearer "+s.APIKey)
	extra := s.AdditionalHeaders
	if opts.AdditionalHeaders != nil {
		extra = opts.AdditionalHeaders
	}
	for k, v := range extra {
		h.Set(k, v)
	}
	h.Del(R3StoreHeader)
	h.Del(R3TTLHeader)
	if opts.LogicalRequestID != "" {
		h.Set("X-Request-Id", opts.LogicalRequestID)
	}
	return h
}
func (s *DeploymentSampler) routingRequest(ctx context.Context, payload map[string]any, opts CompletionRequestOptions) (http.Header, error) {
	if _, exists := opts.Extra["routing_matrix_format"]; exists {
		return nil, fmt.Errorf("routing_matrix_format is selected by the trainer-bound sampler")
	}
	ttl := opts.R3TTLSeconds
	if raw, exists := payload["r3_ttl_seconds"]; exists {
		n, ok := intFromStrictAny(raw)
		if !ok || n <= 0 {
			return nil, fmt.Errorf("r3_ttl_seconds must be a positive integer")
		}
		ttl = &n
		delete(payload, "r3_ttl_seconds")
	}
	if ttl != nil && *ttl <= 0 {
		return nil, fmt.Errorf("r3_ttl_seconds must be a positive integer")
	}
	headers := s.completionHeaders(opts)
	if err := normalizeCaptureFormats(payload); err != nil {
		return nil, err
	}
	routing, _ := payload["include_routing_matrix"].(bool)
	capture := payload["top_sampling_format"] == "parquet_v1" || payload["sample_extra_format"] == "parquet_v1" || payload["sampling_mask"] == "parquet_v1"
	if !routing && !capture {
		return headers, nil
	}
	format, store, required := s.routingBinding()
	if required || format != RoutingParquetV1 || store == "" {
		return nil, fmt.Errorf("capture requires a sampler bound to trainer shared storage")
	}
	if routing {
		delete(payload, "include_routing_matrix")
		payload["routing_matrix_format"] = "parquet_v1"
	}
	headers.Set(R3StoreHeader, store)
	if ttl != nil {
		headers.Set(R3TTLHeader, strconv.Itoa(*ttl))
	}
	return headers, nil
}

func validateRoutingData(data []TrainingDatum, format RoutingMatrixFormat, store string) error {
	for _, datum := range data {
		if datum.ModelInput["routing_matrices"] != nil {
			return fmt.Errorf("inline routing matrices are unsupported")
		}
		for _, field := range []string{"routing_references", "top_sampling_references", "sampling_mask_references", "sample_extra_references"} {
			raw := datum.ModelInput[field]
			if raw == nil {
				continue
			}
			if field == "routing_references" && fmt.Sprint(datum.ModelInput["routing_matrix_format"]) != "parquet_v1" {
				return fmt.Errorf("R3 references require parquet_v1")
			}
			if store == "" {
				return fmt.Errorf("%s requires trainer shared storage", field)
			}
			refs, err := ParseRoutingReferences(raw)
			if err != nil {
				return err
			}
			count, ok := modelInputPositionCount(datum.ModelInput)
			if !ok || count != refs.Length {
				return fmt.Errorf("%s length must match model input length", field)
			}
			for _, file := range refs.Files {
				if file["store_id"] != store {
					return fmt.Errorf("%s belongs to a different storage domain", field)
				}
			}
		}
	}
	return nil
}

// RoutingFromWire decodes compact routing references. The legacy slice result is always nil.
func RoutingFromWire(data json.RawMessage) ([]string, *RoutingReferences, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil, nil, nil
	}
	if data[0] != '{' {
		return nil, nil, fmt.Errorf("inline routing matrices are unsupported")
	}
	r, err := ParseRoutingReferences(data)
	return nil, r, err
}

func normalizeCaptureFormats(payload map[string]any) error {
	for _, pair := range [][2]string{{"top_sampling_logprobs", "top_sampling_format"}, {"sample_extra_tokens", "sample_extra_format"}} {
		raw, exists := payload[pair[0]]
		count := 0
		if exists {
			var ok bool
			count, ok = intFromStrictAny(raw)
			if !ok || count < 0 {
				return fmt.Errorf("%s must be a nonnegative integer", pair[0])
			}
		}
		format := payload[pair[1]]
		if format != nil && (format != "parquet_v1" || count == 0) {
			return fmt.Errorf("invalid %s", pair[1])
		}
		if count > 0 {
			payload[pair[1]] = "parquet_v1"
		}
	}
	switch payload["sampling_mask"] {
	case nil, false:
		delete(payload, "sampling_mask")
	case true, "parquet_v1":
		payload["sampling_mask"] = "parquet_v1"
	default:
		return fmt.Errorf("sampling_mask must request shared-storage capture")
	}
	if payload["sample_extra_format"] == "parquet_v1" {
		if payload["top_sampling_format"] != nil {
			return fmt.Errorf("extra and top sampling captures are mutually exclusive")
		}
		if payload["logprobs"] != true {
			return fmt.Errorf("extra sampling requires logprobs")
		}
		if (payload["temperature"] != nil && floatFromAny(payload["temperature"]) <= 0) || (payload["top_p"] != nil && floatFromAny(payload["top_p"]) <= 0) || floatFromAny(payload["min_p"]) >= 1 || floatFromAny(payload["top_k"]) == 1 {
			return fmt.Errorf("extra sampling requires stochastic decoding")
		}
	}
	return nil
}

func scoreCenteringEstimator(config map[string]any) (string, error) {
	enabled := false
	if raw, ok := config["score_centering"]; ok {
		var valid bool
		enabled, valid = raw.(bool)
		if !valid {
			return "", fmt.Errorf("score_centering must be a boolean")
		}
	}
	est, exists := config["score_centering_estimator"]
	if !enabled {
		if exists || floatFromAny(config["sampler_kl_coeff"]) != 0 {
			return "", fmt.Errorf("estimator and sampler KL require score centering")
		}
		return "", nil
	}
	if !exists || est == "extra_sampling" {
		if _, set := config["tail_mass_epsilon"]; set {
			return "", fmt.Errorf("tail_mass_epsilon requires top_sampling")
		}
		return "extra_sampling", nil
	}
	if est == "top_sampling" {
		return "top_sampling", nil
	}
	return "", fmt.Errorf("unsupported score_centering_estimator")
}
func validateScoreCenteringInputs(data []TrainingDatum, config map[string]any) error {
	est, err := scoreCenteringEstimator(config)
	if err != nil {
		return err
	}
	for i, datum := range data {
		top := datum.ModelInput["top_sampling_references"] != nil
		extra := datum.ModelInput["sample_extra_references"] != nil
		if (est == "top_sampling") != top || (est == "extra_sampling") != extra {
			return fmt.Errorf("datum %d: score centering capture does not match estimator %q", i, est)
		}
	}
	return nil
}

func ModelInputFromIntsWithExtraSampling(tokens []int, refs *RoutingReferences) (map[string]any, error) {
	input := ModelInputFromInts(tokens)
	if refs != nil {
		if err := refs.Validate(); err != nil {
			return nil, err
		}
		if len(tokens) != refs.Length {
			return nil, fmt.Errorf("extra sampling references must match model input length")
		}
		input["sample_extra_references"] = refs.Compact()
	}
	return input, nil
}
