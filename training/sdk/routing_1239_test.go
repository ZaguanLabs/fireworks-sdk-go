package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRoutingWireAndGapOnlyConcat(t *testing.T) {
	for _, wire := range []string{`[]`, `[""]`, `["inline"]`} {
		if _, _, err := RoutingFromWire(json.RawMessage(wire)); err == nil {
			t.Fatalf("accepted inline wire %s", wire)
		}
	}
	if _, err := ConcatRouting([]string{"inline"}); err == nil {
		t.Fatal("accepted a list of inline rows")
	}
	value, err := ConcatRouting([]string{""}, []string{"", ""})
	refs, ok := value.(*RoutingReferences)
	if err != nil || !ok || refs.Length != 3 || !refs.HasGaps() || len(refs.Spans) != 1 {
		t.Fatalf("unexpected compact gap references: %#v, %v", value, err)
	}
	if _, found := refs.ModelInputKwargs()["routing_matrices"]; found {
		t.Fatal("retired model input wire field present")
	}
}

type closingAuditTransport struct {
	http.RoundTripper
	closed bool
}

func (r *closingAuditTransport) CloseIdleConnections() { r.closed = true }

func TestReusedSamplerHandleOwnsBindingAndConcurrency(t *testing.T) {
	ctx := context.Background()
	training := &FiretitanTrainingClient{RoutingMatrixFormat: RoutingParquetV1, R3StoreID: "trainer"}
	svc := &FiretitanServiceClient{trainingClients: []*FiretitanTrainingClient{training}}
	for _, model := range []string{"accounts/a/trainingSamplers/sample", "accounts/a/trainingSessions/ts-remote", "accounts/a/trainingSessions/ts-remote/checkpoints/step-1"} {
		transport := &closingAuditTransport{RoundTripper: http.DefaultTransport}
		base := NewDeploymentSampler("https://example.invalid", model, "key", WithDeploymentSamplerHTTPClient(&http.Client{Transport: transport}))
		base.NegotiateRoutingMatrixFormat(RoutingParquetV1, "source")
		base.r3BindingRequired = true
		handle, err := svc.CreateSamplingClient(ctx, "", nil, BackendManagedConcurrency{}, base)
		if err != nil {
			t.Fatal(err)
		}
		clone := handle.DeploymentSampler
		if clone == base || clone.R3StoreID() != "" || clone.r3BindingRequired || !clone.backendManagedConcurrency || clone.ConcurrencyController != nil || base.backendManagedConcurrency || base.R3StoreID() != "source" {
			t.Fatal("a remote sampler inherited stale routing or changed the original handle")
		}
		controller := NewFixedConcurrencyController(2)
		bound, err := svc.CreateSamplingClientForTraining(ctx, "", nil, controller, training, clone)
		if err != nil {
			t.Fatal(err)
		}
		if bound.DeploymentSampler.R3StoreID() != "trainer" || bound.DeploymentSampler.ConcurrencyController != controller || bound.DeploymentSampler.backendManagedConcurrency || clone.R3StoreID() != "" {
			t.Fatal("explicit trainer and scheduler did not override the new handle's defaults")
		}
		bound.Close()
		handle.Close()
		if transport.closed {
			t.Fatal("closing a handle closed a borrowed transport")
		}
		base.Close()
		if !transport.closed {
			t.Fatal("source sampler lost transport ownership")
		}
	}
}
