package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type parity1219Backend struct {
	request         map[string]any
	failure         error
	forwardOptions  ForwardBackwardOptions
	backwardOptions ForwardBackwardOptions
}

func (b *parity1219Backend) OptimStep(context.Context, OptimStepOptions) (map[string]any, error) {
	return nil, nil
}
func (b *parity1219Backend) ForwardBackward(_ context.Context, o ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	b.backwardOptions = o
	return ForwardBackwardOutput{}, nil
}
func (b *parity1219Backend) Forward(_ context.Context, o ForwardBackwardOptions) (ForwardBackwardOutput, error) {
	b.forwardOptions = o
	return ForwardBackwardOutput{LossFnOutputs: []map[string]any{{"projection": TensorData{Data: []float32{1, 2, 3, 4}, Shape: []int{2, 2}, DType: "float32"}}}}, nil
}
func (b *parity1219Backend) WeightSync(_ context.Context, request map[string]any) (WeightSyncResponse, error) {
	b.request = request
	return WeightSyncResponse{Version: "rdma:1"}, b.failure
}

func Test1219ProjectionConfigAndGradients(t *testing.T) {
	dim := 2
	config, err := (FiretitanProvisioningConfig{BaseModel: "base", ProjectionHeadDim: &dim, LoraRank: 8, LoraInitMethod: "nora", ExtraArgs: []string{"--existing"}}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	body := config.CreateModelRequestBody("session", 1, nil)
	if body["projection_head_dim"] != 2 || body["lora_config"].(map[string]any)["init_method"] != "nora" {
		t.Fatal(body)
	}
	if !reflect.DeepEqual(TrainerExtraArgs(config), []string{"--existing", "--projection-head-dim=2"}) {
		t.Fatal(TrainerExtraArgs(config))
	}
	if len(config.ExtraArgs) != 1 {
		t.Fatal("mutated arguments")
	}
	reference, err := ReferenceManagedConfig(config, 8)
	if err != nil || reference.ProjectionHeadDim != nil {
		t.Fatalf("%+v %v", reference, err)
	}
	other := config
	other.LoraInitMethod = "kaiming"
	if ManagedTrainingClientKey(other) == ManagedTrainingClientKey(config) {
		t.Fatal("initialization missing from duplicate key")
	}
	other = config
	other.ProjectionHeadDim = nil
	if ManagedTrainingClientKey(other) == ManagedTrainingClientKey(config) {
		t.Fatal("projection missing from duplicate key")
	}
	negative := -1
	if _, err := (FiretitanProvisioningConfig{ProjectionHeadDim: &negative}).Normalize(); err == nil {
		t.Fatal("negative projection accepted")
	}
	backend := &parity1219Backend{}
	client := &FiretitanTrainingClient{ComputeBackend: backend, ModelID: "m"}
	data := []TrainingDatum{{ModelInput: ModelInputFromInts([]int{1, 2})}}
	result, err := client.ForwardBackwardCustom(context.Background(), data, func(_ []TrainingDatum, tensors []TensorData) ([]TensorData, map[string]float64, error) {
		if !reflect.DeepEqual(tensors[0].Shape, []int{2, 2}) {
			t.Error(tensors)
		}
		return []TensorData{{Data: []float64{.1, .2, .3, .4}, Shape: []int{2, 2}}}, map[string]float64{"loss": 3}, nil
	}, "projection", EmbeddingPoolingMean)
	if err != nil {
		t.Fatal(err)
	}
	if backend.forwardOptions.LossFnConfig["output"] != "projection" || backend.backwardOptions.LossFnConfig["output"] != "projection" || result.Metrics["loss"] != 3 {
		t.Fatal(backend, result)
	}
	grad := backend.backwardOptions.Data[0].LossFnInputs["projection_grads"]
	if !reflect.DeepEqual(grad.Shape, []int{2, 2}) || grad.DType != "float32" || len(grad.Data.([]float32)) != 4 {
		t.Fatal(grad)
	}
	if _, err := client.ForwardProjection(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

func Test1219CapabilitiesAndModelErrors(t *testing.T) {
	for _, raw := range []string{`{}`, `{"supports_router_replay":"true","supports_rdma_weight_sync":1}`} {
		var value CreateModelResponse
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if value.SupportsRouterReplay != nil || value.SupportsRDMAWeightSync {
			t.Fatal(value)
		}
	}
	var value CreateModelResponse
	if err := json.Unmarshal([]byte(`{"supports_router_replay":false,"supports_rdma_weight_sync":true}`), &value); err != nil {
		t.Fatal(err)
	}
	if value.SupportsRouterReplay == nil || *value.SupportsRouterReplay || !value.SupportsRDMAWeightSync {
		t.Fatal(value)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer server.Close()
	_, err := NewFireworksClient("key", server.URL).ModelIsMoE(context.Background(), "accounts/a/models/private")
	var unavailable *ModelDetailsUnavailableError
	if !errors.As(err, &unavailable) || unavailable.StatusCode != 403 {
		t.Fatal(err)
	}
}

func Test1219RDMANegotiationAndFallback(t *testing.T) {
	for _, mode := range []string{"rdma", "old", "mixed", "fanout", "lora", "cmek", "serverless", "disabled", "route404", "route405", "route500", "completion"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state := map[string]any{"replicas": []any{map[string]any{"supports_rdma_weight_sync": true}}}
				switch mode {
				case "old":
					state["replicas"] = []any{map[string]any{}}
				case "mixed":
					state["replicas"] = []any{map[string]any{"supports_rdma_weight_sync": true}, map[string]any{"supports_rdma_weight_sync": false}}
				case "fanout":
					state["replica_distribution"] = map[string]any{}
				}
				json.NewEncoder(w).Encode(state)
			}))
			defer server.Close()
			manager := NewDeploymentManager("key", server.URL, WithDeploymentHotloadAPIURL(server.URL))
			manager.SetAccountID("acct")
			hotloads := 0
			sampler := &TinkerSamplerBackend{DeployMgr: manager, DeploymentID: "dep", BaseModel: "base", HotloadAndWait: func(context.Context, string, string, string, ...HotloadAndWaitOptions) (bool, error) {
				hotloads++
				return true, nil
			}}
			transport := &parity1219Backend{}
			saver := &fakeSamplerSaver{}
			client := &FiretitanTrainingClient{ModelID: "m", SamplerBackend: sampler, WeightSyncer: NewWeightSyncer(WeightSyncerConfig{PolicyClient: saver}), ComputeBackend: transport, trainerSupportsRDMA: true}
			switch mode {
			case "lora":
				client.Config.LoraRank = 8
			case "cmek":
				sampler.CMEKResource = "encrypted"
				sampler.HotLoadBucketURL = "gs://encrypted-bucket/"
			case "serverless":
				client.RunName = "run"
			case "disabled":
				sampler.AllowRDMA = boolPointer(false)
			case "route404":
				transport.failure = &WeightSyncRouteUnavailableError{404}
			case "route405":
				transport.failure = &WeightSyncRouteUnavailableError{405}
			case "route500":
				transport.failure = &WeightSyncRouteUnavailableError{500}
			case "completion":
				transport.failure = errors.New("accepted publication failed")
			}
			result, err := client.WeightSync(context.Background())
			if mode == "completion" || mode == "route500" {
				if err == nil || len(saver.calls) != 0 || hotloads != 0 {
					t.Fatalf("unsafe retry: %v %d %d", err, len(saver.calls), hotloads)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "rdma" {
				if result.Version != "rdma:1" || len(saver.calls) != 0 || !client.SupportsRDMAWeightSync() {
					t.Fatal(result)
				}
				session := transport.request["session_id"]
				epoch := transport.request["source_epoch"]
				operation := transport.request["operation_id"]
				if _, err = client.WeightSync(context.Background()); err != nil {
					t.Fatal(err)
				}
				if transport.request["session_id"] != session || transport.request["source_epoch"] != epoch || transport.request["operation_id"] == operation {
					t.Fatal("RDMA session continuity failed")
				}
			} else {
				if len(saver.calls) != 1 || hotloads != 1 || result.OptimizerVersion != nil {
					t.Fatalf("fallback %d %d %+v", len(saver.calls), hotloads, result)
				}
			}
		})
	}
}

func Test1219SamplerCloseAndDrain(t *testing.T) {
	for _, mode := range []string{"cancel", "drain", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			client := NewFiretitanSamplingClient(nil)
			ctx, finish, err := client.beginRequest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			opts := SamplingCloseOptions{Drain: mode != "cancel", DrainTimeout: 20 * time.Millisecond}
			client.Close(opts)
			if _, _, err := client.beginRequest(context.Background()); err == nil {
				t.Fatal("closed client accepted work")
			}
			if mode == "drain" {
				if ctx.Err() != nil {
					t.Fatal("drain cancelled active request")
				}
				finish()
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("pending request stranded")
			}
			var closed *SamplingClientClosedError
			if !errors.As(context.Cause(ctx), &closed) {
				t.Fatal(context.Cause(ctx))
			}
			finish()
			client.Close()
		})
	}
}

func Test1219TrainerChartOverrides(t *testing.T) {
	config := FiretitanProvisioningConfig{BaseModel: "base", ProjectionHeadDim: intPointer(4), ExtraValues: map[string]string{"rdmaWeightSyncEnabled": "false"}}
	if RDMACandidate(config) {
		t.Fatal("explicit opt-out ignored")
	}
	if !strings.Contains(strings.Join(TrainerExtraArgs(config), " "), "--projection-head-dim=4") {
		t.Fatal("missing projection flag")
	}
}
