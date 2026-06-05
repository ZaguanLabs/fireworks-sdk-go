package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShouldVerifySSL(t *testing.T) {
	tests := map[string]bool{
		"https://api.fireworks.ai":  true,
		"http://203.0.113.10:8083":  false,
		"https://203.0.113.10:8083": false,
		"https://127.0.0.1:8080":    false,
		"https://example.com":       true,
	}
	for rawURL, want := range tests {
		if got := ShouldVerifySSL(rawURL); got != want {
			t.Fatalf("ShouldVerifySSL(%q) = %t, want %t", rawURL, got, want)
		}
	}
}

func TestTrainingRestClientHeaders(t *testing.T) {
	client := NewTrainingRestClient(
		"test-key",
		"https://api.example.com/",
		WithTrainingAdditionalHeaders(map[string]string{"X-Secret": "s"}),
	)
	headers := client.Headers(map[string]string{"Authorization": "Bearer test-key"})
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := headers.Get("X-Api-Key"); got != "test-key" {
		t.Fatalf("X-Api-Key = %q", got)
	}
	if got := headers.Get("X-Secret"); got != "s" {
		t.Fatalf("X-Secret = %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if client.BaseURL() != "https://api.example.com" {
		t.Fatalf("base URL = %q", client.BaseURL())
	}
}

func TestTrainingRestClientGetAndPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q", got)
		}
		switch r.URL.Path {
		case "/get":
			if r.Method != http.MethodGet {
				t.Errorf("method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/post":
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body["name"] != "trainer" {
				t.Errorf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"created": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTrainingRestClient("test-key", server.URL)
	resp, err := client.Get(context.Background(), "/get", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}

	resp, err = client.Post(context.Background(), "/post", map[string]string{"name": "trainer"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}
}

func TestTrainingRestClientResolveAccountID(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/accounts" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("pageSize"); got != "2" {
			t.Errorf("pageSize = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accounts": []map[string]string{{"name": "accounts/acct-1"}},
		})
	}))
	defer server.Close()

	client := NewTrainingRestClient("test-key", server.URL)
	accountID, err := client.AccountID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "acct-1" {
		t.Fatalf("account ID = %q", accountID)
	}
	accountID, err = client.AccountID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "acct-1" || requests != 1 {
		t.Fatalf("account ID = %q requests = %d", accountID, requests)
	}
}

func TestTrainingRestClientSetAccountIDSkipsResolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s", r.URL.String())
	}))
	defer server.Close()

	client := NewTrainingRestClient("test-key", server.URL)
	client.SetAccountID("manual")
	accountID, err := client.AccountID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "manual" {
		t.Fatalf("account ID = %q", accountID)
	}
}

func TestTrainingRestClientResolveAccountIDErrors(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "no accounts",
			body: map[string]any{"accounts": []map[string]string{}},
			want: "not associated",
		},
		{
			name: "multiple accounts",
			body: map[string]any{"accounts": []map[string]string{{"name": "accounts/a"}, {"name": "accounts/b"}}},
			want: "multiple accounts",
		},
		{
			name: "empty parsed account",
			body: map[string]any{"accounts": []map[string]string{{"name": "accounts/"}}},
			want: "Could not parse account ID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(test.body)
			}))
			defer server.Close()

			client := NewTrainingRestClient("test-key", server.URL)
			_, err := client.ResolveAccountID(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestTrainingRestClientVerifyOverride(t *testing.T) {
	client := NewTrainingRestClient("test-key", "https://api.example.com", WithTrainingVerifySSL(false))
	if client.VerifyForURL("https://api.example.com") {
		t.Fatal("expected verify override false")
	}
	client = NewTrainingRestClient("test-key", "http://127.0.0.1:8080", WithTrainingVerifySSL(true))
	if !client.VerifyForURL("http://127.0.0.1:8080") {
		t.Fatal("expected verify override true")
	}
}
