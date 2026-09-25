package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testRouting(length int) *RoutingReferences {
	return &RoutingReferences{Length: length, Files: []RoutingFile{{"format": "parquet_v1", "store_id": "store", "row_count": length, "uri": "s3://bucket/routes.parquet"}}, Spans: []RoutingSpan{{InputTokenStart: 0, Count: length, FileIndex: intPointer(0), FileRowStart: intPointer(0)}}}
}
func TestCompactRoutingRoundTripSliceAndMask(t *testing.T) {
	r := testRouting(1000000)
	a, err := r.Slice(5, 9)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Slice(9, 12)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := ConcatRouting(a, b)
	if err != nil {
		t.Fatal(err)
	}
	compact := joined.(*RoutingReferences)
	if compact.Length != 7 || len(compact.Files) != 1 || len(compact.Spans) != 1 || *compact.Spans[0].FileRowStart != 5 {
		t.Fatal(compact)
	}
	masked, err := compact.Mask([]bool{true, true, false, false, true, true, true})
	if err != nil {
		t.Fatal(err)
	}
	if !masked.HasGaps() || len(masked.Spans) != 3 || len(masked.Files) != 1 {
		t.Fatal(masked)
	}
	wire, err := json.Marshal(masked)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseRoutingReferences(json.RawMessage(wire))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Length != 7 || len(wire) > 700 {
		t.Fatalf("references expanded: %s", wire)
	}
	if _, err := ConcatRouting(a, []string{"inline"}); err == nil {
		t.Fatal("mixed inline/Parquet accepted")
	}
	for _, bad := range []string{
		`{}`, `{"length":1,"files":[],"spans":[]}`,
		`{"length":1,"files":[],"spans":[{"input_token_start":0,"count":1,"file_index":0,"file_row_start":0}]}`,
		`{"length":1,"files":[{"format":"parquet_v1","row_count":0}],"spans":[{"input_token_start":0,"count":1,"file_index":0,"file_row_start":0}]}`,
	} {
		if _, err := ParseRoutingReferences(json.RawMessage(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestR3NegotiationAndReplicaFallback(t *testing.T) {
	for _, mode := range []string{"parquet", "old", "replica", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var probes, real atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				unsupported := func() {
					w.WriteHeader(400)
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "INVALID_ARGUMENT", "message": "Extra inputs are not permitted, field: 'routing_matrix_format', value: 'parquet_v1'"}})
				}
				if body["prompt"] == "R3 compatibility check." {
					probes.Add(1)
					if r.Header.Get(R3TTLHeader) != "60" || r.Header.Get(R3StoreHeader) != "store" || body["max_tokens"] != float64(0) {
						t.Errorf("bad probe %v %v", body, r.Header)
					}
					if mode == "old" {
						unsupported()
						return
					}
					store := "store"
					if mode == "invalid" {
						store = "wrong"
					}
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"routing_matrix_format": "parquet_v1", "r3_store_id": store, "routing_references": testRouting(1)}}})
					return
				}
				n := real.Add(1)
				if mode == "replica" && n == 1 {
					unsupported()
					return
				}
				if mode == "old" || mode == "replica" {
					if body["include_routing_matrix"] != true || body["routing_matrix_format"] != nil || r.Header.Get(R3StoreHeader) != "" || r.Header.Get(R3TTLHeader) != "" {
						t.Errorf("inline fallback %v %v", body, r.Header)
					}
				} else if r.Header.Get(R3TTLHeader) != "300" || r.Header.Get(R3StoreHeader) != "store" || body["routing_matrix_format"] != "parquet_v1" || body["r3_ttl_seconds"] != nil {
					t.Errorf("Parquet request %v %v", body, r.Header)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"stop\",\"raw_output\":{\"completion_token_ids\":[2]},\"routing_matrix_format\":\"parquet_v1\",\"r3_store_id\":\"store\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			s := NewDeploymentSampler(server.URL, "model", "key")
			defer s.Close()
			s.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
			opts := CompletionRequestOptions{IncludeRoutingMatrix: true, R3TTLSeconds: intPointer(300), AdditionalHeaders: map[string]string{R3StoreHeader: "attacker", R3TTLHeader: "1"}}
			_, _, err := s.StreamCompletions(context.Background(), []int{1}, opts)
			if mode == "invalid" {
				if err == nil || real.Load() != 0 {
					t.Fatal("invalid probe did not stop request")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if probes.Load() != 1 {
				t.Fatal("probe count", probes.Load())
			}
			if mode != "replica" {
				_, _, err = s.StreamCompletions(context.Background(), []int{1}, opts)
				if err != nil {
					t.Fatal(err)
				}
				if probes.Load() != 1 {
					t.Fatal("repeated probe")
				}
			}
		})
	}
}

func TestR3SharedProbeSurvivesCallerCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if probes.Add(1) == 1 {
			close(started)
		}
		<-release
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"routing_matrix_format": "parquet_v1", "r3_store_id": "store", "routing_references": testRouting(1)}}})
	}))
	defer server.Close()
	s := NewDeploymentSampler(server.URL, "model", "key")
	defer s.Close()
	s.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- s.ensureRoutingFormat(ctx, http.Header{}) }()
	<-started
	second := make(chan error, 1)
	go func() { second <- s.ensureRoutingFormat(context.Background(), http.Header{}) }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 || s.RoutingMatrixFormat() != RoutingParquetV1 {
		t.Fatal("shared negotiation lost")
	}
}

func TestTopLogprobsAndPartialEchoAlignment(t *testing.T) {
	s := NewDeploymentSampler("http://unused", "model", "key")
	defer s.Close()
	s.NegotiateRoutingMatrixFormat(RoutingParquetV1, "store")
	content := []any{}
	for _, id := range []int{12, 20, 21} {
		content = append(content, map[string]any{"token_id": id, "logprob": -0.1, "sampling_logprob": -0.2, "top_logprobs": []any{map[string]any{"token_id": id, "logprob": -0.1}, map[string]any{"token_id": 99, "logprob": -2.0}}})
	}
	choice := map[string]any{"raw_output": map[string]any{"completion_token_ids": []int{12, 20, 21}}, "logprobs": map[string]any{"content": content}, "routing_matrix_format": "parquet_v1", "r3_store_id": "store", "routing_references": testRouting(3)}
	result := map[string]any{"choices": []any{choice}}
	out, err := s.ParseCompletionsResultWithEchoLast(result, []int{10, 11, 12}, 0, true, true, false, intPointer(1))
	if err != nil {
		t.Fatal(err)
	}
	c := out[0]
	if c.EchoedPromptLogprobCount != 1 || c.CompletionLen != 2 || len(c.InferenceTopKTokenIDs) != 3 || c.RoutingReferences.Length != 3 {
		t.Fatalf("completion %+v", c)
	}
	if !reflect.DeepEqual(c.InferenceTopKTokenIDs[0], []int{12, 99}) {
		t.Fatal(c.InferenceTopKTokenIDs)
	}
	choice["r3_store_id"] = "wrong"
	if _, err := s.ParseCompletionsResult(result, []int{10, 11, 12}, 0, true, true, false); err == nil {
		t.Fatal("wrong storage accepted")
	}
}

func TestModelCapabilitiesStayLocalAndPreserveWire(t *testing.T) {
	var wg sync.WaitGroup
	for _, version := range []Comms{CommsV1, CommsV2, "future"} {
		wg.Add(1)
		go func(version Comms) {
			defer wg.Done()
			c := &FiretitanTrainingClient{}
			c.ApplyModelCreationResponse(CreateModelResponse{ModelID: "m", Comms: version, RoutingMatrixFormat: RoutingParquetV1, R3StoreID: "store"})
			body := (ForwardBackwardOptions{ModelID: c.ModelID, Comms: c.Comms(), SeqID: 1, LossFn: LossFnCrossEntropy}).RequestBody()
			if version == CommsV2 {
				if body["comms"] != "v2" {
					t.Error(body)
				}
			} else if _, ok := body["comms"]; ok {
				t.Error("v1 gained comms")
			}
			input := ModelInputFromInts([]int{1, 2})
			for k, v := range testRouting(2).ModelInputKwargs() {
				input[k] = v
			}
			if err := c.ValidateRoutingData([]TrainingDatum{{ModelInput: input}}); err != nil {
				t.Error(err)
			}
			c.R3StoreID = "other"
			if err := c.ValidateRoutingData([]TrainingDatum{{ModelInput: input}}); err == nil {
				t.Error("cross-store trainer request accepted")
			}
		}(version)
	}
	wg.Wait()
	weights, err := LinearLossWeights([]float32{1, 2, 3, 4}, []int{2, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(weights.Data, []float32{-1, -2, -3, -4}) || !reflect.DeepEqual(weights.Shape, []int{2, 2}) {
		t.Fatal(weights)
	}
}

func TestChildTrainerFailurePreservesCanonicalStatus(t *testing.T) {
	err := ChildJobFailedError("job", map[string]any{"status": map[string]any{"code": "FAILED_PRECONDITION", "message": "capacity unavailable", "details": []any{map[string]any{"reason": "capacity"}}}})
	var status *TrainingAPIError
	if !errors.As(err, &status) || status.LifecycleStatus == nil || status.LifecycleStatus.GRPCCode() != 9 || status.LifecycleStatus.PublicMessage() != "capacity unavailable" {
		t.Fatal(err)
	}
}

func TestReattachWaitsBeforePatchAndFailsTerminal(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		mgr := NewDeploymentManager("key", "http://unused")
		now := time.Unix(0, 0)
		reads := 0
		patched := false
		_, err := mgr.ReattachTrainer(context.Background(), DeploymentInfo{DeploymentID: "dep"}, "model", "new", ReattachTrainerOptions{
			Now: func() time.Time { return now }, Sleep: func(d time.Duration) { now = now.Add(d) }, Timeout: time.Second, PollInterval: time.Millisecond,
			GetInfo: func(context.Context, string) (DeploymentInfo, bool, error) {
				reads++
				state := "READY"
				job := "old"
				if reads == 1 {
					state = "UPDATING"
				}
				if terminal {
					state = "FAILED"
				}
				if patched {
					job = "new"
				}
				return DeploymentInfo{DeploymentID: "dep", State: state, HotLoadTrainerJob: job}, true, nil
			},
			Update: func(context.Context, string, map[string]any, any) (DeploymentInfo, error) {
				if reads < 2 {
					t.Error("patched before READY")
				}
				patched = true
				return DeploymentInfo{}, nil
			},
		})
		if terminal {
			if err == nil || patched {
				t.Fatal("terminal deployment patched")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagedJobCheckpointPaginationAndPromotion(t *testing.T) {
	for _, kind := range []string{"supervisedFineTuningJobs", "dpoJobs"} {
		t.Run(kind, func(t *testing.T) {
			parent := "accounts/acct/" + kind + "/job"
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/"+parent+"/checkpoints":
					if r.URL.Query().Get("pageSize") != "7" {
						t.Errorf("query %s", r.URL.RawQuery)
					}
					if r.URL.Query().Get("pageToken") == "next" {
						fmt.Fprint(w, `{"checkpoints":[{"name":"second"}]}`)
					} else {
						fmt.Fprint(w, `{"checkpoints":[{"name":"first"}],"nextPageToken":"next"}`)
					}
				case r.Method == http.MethodPost && r.URL.Path == "/v1/"+parent+"/checkpoints/first:promote":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["async_promotion"] != true || body["output_model"] != "accounts/acct/models/output" || body["base_model"] != "base" {
						t.Errorf("body %v", body)
					}
					fmt.Fprint(w, `{"operation":{"name":"accounts/acct/operations/promote","done":true,"response":{"@type":"Model","name":"accounts/acct/models/output"}}}`)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			client := NewFireworksClient("key", server.URL)
			rows, err := client.ListTrainingJobCheckpoints(context.Background(), parent, 7)
			if err != nil || len(rows) != 2 {
				t.Fatalf("rows %v error %v", rows, err)
			}
			model, err := client.PromoteTrainingJobCheckpoint(context.Background(), parent+"/checkpoints/first", "output", "base")
			if err != nil || model["name"] != "accounts/acct/models/output" || model["@type"] != nil {
				t.Fatalf("model %v error %v", model, err)
			}
			if calls != 3 {
				t.Fatal(calls)
			}
		})
	}
}

func TestSampledSequenceRoutingRoundTrip(t *testing.T) {
	original := FiretitanSampledSequence{RoutingReferences: testRouting(3)}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FiretitanSampledSequence
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.RoutingReferences, decoded.RoutingReferences) {
		// Dynamic file metadata decodes JSON numeric values as float64.
		encoded, _ := json.Marshal(decoded)
		if string(data) != string(encoded) {
			t.Fatalf("%s != %s", data, encoded)
		}
	}
}

func TestSamplerBindingRetainsCustomRequester(t *testing.T) {
	called := false
	s := NewDeploymentSampler("http://unused", "model", "key", WithDeploymentSamplerRequester(func(context.Context, []int, CompletionRequestOptions) (map[string]any, ServerMetrics, error) {
		called = true
		return nil, ServerMetrics{}, nil
	}))
	clone := s.CopyForR3Binding(RoutingMatrixFormat("parquet_v1"), "store")
	clone.CompletionRequester(context.Background(), nil, CompletionRequestOptions{})
	if !called {
		t.Fatal("custom requester lost")
	}
	if s.R3StoreID() != "" || clone.R3StoreID() != "store" {
		t.Fatal("binding leaked")
	}
}

type orderingTrainer struct {
	fakeManagedTrainer
	deploymentStarted chan struct{}
	ready             atomic.Bool
	sequential        bool
}

func (t *orderingTrainer) WaitForReady(ctx context.Context, jobID, jobName string, opts ...TrainerPollOptions) (TrainerServiceEndpoint, error) {
	if !t.sequential {
		select {
		case <-t.deploymentStarted:
		case <-ctx.Done():
			return TrainerServiceEndpoint{}, ctx.Err()
		}
	}
	t.ready.Store(true)
	return t.fakeManagedTrainer.WaitForReady(ctx, jobID, jobName, opts...)
}

type orderingDeployment struct {
	fakeManagedDeployment
	trainer *orderingTrainer
}

func (d *orderingDeployment) CreateOrGet(ctx context.Context, config DeploymentConfig, allow bool) (DeploymentInfo, error) {
	if d.trainer.sequential && !d.trainer.ready.Load() {
		return DeploymentInfo{}, fmt.Errorf("deployment started before trainer was ready")
	}
	close(d.trainer.deploymentStarted)
	return d.fakeManagedDeployment.CreateOrGet(ctx, config, allow)
}
func TestManagedProvisioningReadinessOrdering(t *testing.T) {
	for _, sequential := range []bool{false, true} {
		t.Run(fmt.Sprint(sequential), func(t *testing.T) {
			trainer := &orderingTrainer{deploymentStarted: make(chan struct{}), sequential: sequential}
			deployment := &orderingDeployment{trainer: trainer}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := ProvisionManagedHandle(ctx, ManagedProvisionOptions{Config: FiretitanProvisioningConfig{BaseModel: "base", DeploymentShape: "accounts/acct/deploymentShapes/shape", WaitForTrainerBeforeDeployment: sequential}, Trainer: trainer, Deployment: deployment})
			if err != nil {
				t.Fatal(err)
			}
			if !trainer.ready.Load() || len(deployment.created) != 1 {
				t.Fatal("provisioning incomplete")
			}
		})
	}
}
