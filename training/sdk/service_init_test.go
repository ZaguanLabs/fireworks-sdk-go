package sdk

import "testing"

func TestNewFiretitanServiceClientInitOptionsSetsFireworksAPIKeyHeader(t *testing.T) {
	opts := NewFiretitanServiceClientInitOptions("fw-key", nil)
	if opts.APIKey != "fw-key" {
		t.Fatalf("api key = %q", opts.APIKey)
	}
	if opts.DefaultHeaders["X-API-Key"] != "fw-key" {
		t.Fatalf("headers = %#v", opts.DefaultHeaders)
	}
	if _, ok := opts.DefaultHeaders["Authorization"]; ok {
		t.Fatalf("headers should not include Authorization: %#v", opts.DefaultHeaders)
	}
	if !opts.ClientConfig["use_pyqwest_transport"] || !opts.ClientConfig["parallel_fwdbwd_chunks"] {
		t.Fatalf("client config = %#v", opts.ClientConfig)
	}
	opts.ClientConfig["use_pyqwest_transport"] = false
	if !FiretitanTinkerClientConfig["use_pyqwest_transport"] {
		t.Fatal("client config was not cloned")
	}
}

func TestNewFiretitanServiceClientInitOptionsPreservesExistingHeader(t *testing.T) {
	headers := map[string]string{"X-API-Key": "custom-key", "X-Trace": "trace-1"}
	opts := NewFiretitanServiceClientInitOptions("fw-key", headers)
	if opts.DefaultHeaders["X-API-Key"] != "custom-key" || opts.DefaultHeaders["X-Trace"] != "trace-1" {
		t.Fatalf("headers = %#v", opts.DefaultHeaders)
	}
	opts.DefaultHeaders["X-Trace"] = "mutated"
	if headers["X-Trace"] != "trace-1" {
		t.Fatalf("input headers were not cloned: %#v", headers)
	}
}
