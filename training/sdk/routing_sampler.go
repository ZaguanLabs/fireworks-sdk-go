package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type routingProbe struct {
	done chan struct{}
	err  error
}

// NegotiateRoutingMatrixFormat binds trainer capabilities; the first R3 request
// probes inference. Unknown capabilities retain the inline protocol.
func (s *DeploymentSampler) NegotiateRoutingMatrixFormat(format RoutingMatrixFormat, storeID string) {
	s.r3Mu.Lock()
	defer s.r3Mu.Unlock()
	s.routingMatrixFormat = RoutingBase64Inline
	s.r3StoreID = ""
	if format == RoutingParquetV1 {
		s.r3StoreID = storeID
	}
	s.r3Negotiated = s.r3StoreID == ""
	s.r3Probe = nil
}
func (s *DeploymentSampler) routingBinding() (RoutingMatrixFormat, string, bool) {
	s.r3Mu.Lock()
	defer s.r3Mu.Unlock()
	format := s.routingMatrixFormat
	if format == "" {
		format = RoutingBase64Inline
	}
	return format, s.r3StoreID, s.r3BindingRequired
}
func (s *DeploymentSampler) RoutingMatrixFormat() RoutingMatrixFormat {
	f, _, _ := s.routingBinding()
	return f
}
func (s *DeploymentSampler) R3StoreID() string { _, id, _ := s.routingBinding(); return id }

func (s *DeploymentSampler) CopyForR3Binding(format RoutingMatrixFormat, store string) *DeploymentSampler {
	clone := NewDeploymentSampler(s.InferenceURL, s.Model, s.APIKey)
	// Preserve injected requesters; bind the built-in requester to the new sampler.
	if s.CompletionRequester != nil && reflect.ValueOf(s.CompletionRequester).Pointer() != reflect.ValueOf(s.defaultCompletionRequest).Pointer() {
		clone.CompletionRequester = s.CompletionRequester
	}
	clone.Tokenizer = s.Tokenizer
	clone.ConcurrencyController = s.ConcurrencyController
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
		}
		clone.HTTPClient = &client
	}
	clone.NegotiateRoutingMatrixFormat(format, store)
	return clone
}
func unsupportedRoutingFormat(code int, body []byte) bool {
	if code != 400 {
		return false
	}
	var result struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &result) != nil {
		return false
	}
	return (result.Error.Code == "invalid_request_error" || result.Error.Code == "INVALID_ARGUMENT") && result.Error.Message == "Extra inputs are not permitted, field: 'routing_matrix_format', value: 'parquet_v1'"
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
func (s *DeploymentSampler) postRoutingProbe(ctx context.Context, headers http.Header, store string) error {
	payload := map[string]any{"model": s.Model, "prompt": "R3 compatibility check.", "max_tokens": 0, "n": 1, "echo": true, "stream": false, "logprobs": true, "routing_matrix_format": "parquet_v1"}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", s.InferenceURL+"/inference/v1/completions", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header = headers.Clone()
	req.Header.Set("X-Request-Id", newSamplingRequestID())
	req.Header.Set(R3StoreHeader, store)
	req.Header.Set(R3TTLHeader, "60")
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	format := RoutingBase64Inline
	if !unsupportedRoutingFormat(resp.StatusCode, body) {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &CompletionHTTPStatusError{StatusCode: resp.StatusCode, Body: body, Headers: resp.Header.Clone()}
		}
		var result struct {
			Choices []map[string]any `json:"choices"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		if len(result.Choices) == 0 {
			return fmt.Errorf("R3 probe returned no choice")
		}
		choice := result.Choices[0]
		if choice["routing_matrix_format"] != "parquet_v1" || choice["r3_store_id"] != store {
			return fmt.Errorf("inference did not acknowledge trainer shared storage")
		}
		refs, err := ParseRoutingReferences(choice["routing_references"])
		if err != nil {
			return err
		}
		if len(refs.Files) == 0 {
			return fmt.Errorf("R3 probe published no files")
		}
		for _, f := range refs.Files {
			if f["store_id"] != store {
				return fmt.Errorf("R3 probe used incompatible storage")
			}
		}
		format = RoutingParquetV1
	}
	s.r3Mu.Lock()
	s.routingMatrixFormat = format
	s.r3Negotiated = true
	s.r3Mu.Unlock()
	return nil
}
func (s *DeploymentSampler) ensureRoutingFormat(ctx context.Context, headers http.Header) error {
	s.r3Mu.Lock()
	if s.r3StoreID == "" || s.r3Negotiated {
		s.r3Mu.Unlock()
		return nil
	}
	probe := s.r3Probe
	if probe == nil {
		probe = &routingProbe{done: make(chan struct{})}
		s.r3Probe = probe
		store := s.r3StoreID
		go func() {
			// One cancelled caller must not abort a probe shared by other requests.
			probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			defer cancel()
			err := s.postRoutingProbe(probeCtx, headers, store)
			s.r3Mu.Lock()
			probe.err = err
			if err != nil && s.r3Probe == probe {
				s.r3Probe = nil
			}
			close(probe.done)
			s.r3Mu.Unlock()
		}()
	}
	s.r3Mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-probe.done:
		return probe.err
	}
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
	if wanted, _ := payload["include_routing_matrix"].(bool); !wanted {
		return headers, nil
	}
	_, _, required := s.routingBinding()
	if required {
		return nil, fmt.Errorf("bind sampler to a training client when the service has multiple model handles")
	}
	if err := s.ensureRoutingFormat(ctx, headers); err != nil {
		return nil, err
	}
	format, store, _ := s.routingBinding()
	if format == RoutingParquetV1 {
		delete(payload, "include_routing_matrix")
		payload["routing_matrix_format"] = "parquet_v1"
		headers.Set(R3StoreHeader, store)
		if ttl != nil {
			headers.Set(R3TTLHeader, strconv.Itoa(*ttl))
		}
	}
	return headers, nil
}

func validateRoutingData(data []TrainingDatum, format RoutingMatrixFormat, store string) error {
	for _, datum := range data {
		raw := datum.ModelInput["routing_references"]
		if raw == nil {
			continue
		}
		if format != RoutingParquetV1 || store == "" {
			return fmt.Errorf("Parquet R3 requires trainer support and shared storage")
		}
		if fmt.Sprint(datum.ModelInput["routing_matrix_format"]) != "parquet_v1" {
			return fmt.Errorf("Parquet references require routing_matrix_format=parquet_v1")
		}
		if datum.ModelInput["routing_matrices"] != nil {
			return fmt.Errorf("supply inline routing matrices or references, not both")
		}
		refs, err := ParseRoutingReferences(raw)
		if err != nil {
			return err
		}
		count, ok := modelInputPositionCount(datum.ModelInput)
		if !ok || count != refs.Length {
			return fmt.Errorf("R3 references must match model input length")
		}
		for _, f := range refs.Files {
			if f["store_id"] != store {
				return fmt.Errorf("R3 reference belongs to a different storage domain")
			}
		}
	}
	return nil
}

// RoutingFromWire decodes the upstream routing union without expanding references.
func RoutingFromWire(data json.RawMessage) ([]string, *RoutingReferences, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil, nil
	}
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		r, err := ParseRoutingReferences(data)
		return nil, r, err
	}
	var rows []string
	err := json.Unmarshal(data, &rows)
	return rows, nil, err
}
