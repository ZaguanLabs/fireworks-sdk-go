package sdk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

var sessionCheckpointPattern = regexp.MustCompile(`^(?:accounts/([^/]+)/)?(?:trainingSessions/)?(ts-[A-Za-z0-9-]+)(?:/checkpoints/(.+))?$`)
var checkpointRunPattern = regexp.MustCompile(`^run-[0-9a-f]{32}$`)
var checkpointLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func ParseTrainingSessionCheckpointPath(path string) (account, session, checkpoint string, ok bool) {
	match := sessionCheckpointPattern.FindStringSubmatch(strings.Trim(strings.TrimSpace(path), "/"))
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

func serverlessCallerHeaders(carried map[string]string) map[string]string {
	headers := map[string]string{}
	if value := os.Getenv("CLOUDFLARE_ACCESS_CLIENT_ID"); value != "" {
		headers["CF-Access-Client-Id"] = value
	}
	if value := os.Getenv("CLOUDFLARE_ACCESS_CLIENT_SECRET"); value != "" {
		headers["CF-Access-Client-Secret"] = value
	}
	for key, value := range carried {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "authorization") || strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") {
			continue
		}
		headers[key] = value
	}
	return headers
}
func (c *FiretitanServiceClient) bindStandaloneSamplerRoute(ctx context.Context, body map[string]any) error {
	rest := NewTrainingRestClient(c.Fireworks.APIKey(), c.ManagedBaseURL, WithTrainingAdditionalHeaders(serverlessCallerHeaders(c.AdditionalHeaders)))
	response, err := rest.Post(ctx, "/api/v1/create_sampling_route", body, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		return &NativeTrainingError{Status: response.StatusCode, Message: ParseAPIErrorBody(data)}
	}
	return nil
}
func (c *FiretitanServiceClient) serverlessCheckpointModel(ctx context.Context, path string) (string, error) {
	checkpoint := strings.Trim(strings.TrimSpace(path), "/")
	if _, _, _, explicit := ParseTrainingSessionCheckpointPath(checkpoint); explicit {
		return c.serverlessSamplingModel(ctx, checkpoint)
	}
	parts := strings.Split(checkpoint, "/")
	c.lifecycleMu.Lock()
	clients := append([]*FiretitanTrainingClient(nil), c.trainingClients...)
	c.lifecycleMu.Unlock()
	owned := false
	for _, client := range clients {
		if len(parts) > 1 && client.RunID() == parts[1] {
			owned = true
		}
	}
	if len(parts) < 3 && len(clients) > 0 || owned {
		return c.serverlessSamplingModel(ctx, checkpoint)
	}
	if len(parts) < 3 || !checkpointRunPattern.MatchString(parts[1]) {
		return "", fmt.Errorf("model_path must be a saved sampler checkpoint <account>/<run_id>/<name>")
	}
	for _, part := range parts {
		if !checkpointLabelPattern.MatchString(part) {
			return "", fmt.Errorf("invalid sampler checkpoint path")
		}
	}
	account := c.ResolvedAccountID(ctx)
	if account == "" || parts[0] != account {
		return "", fmt.Errorf("sampler checkpoint account does not match this service")
	}
	if err := c.bindStandaloneSamplerRoute(ctx, map[string]any{"training_run": "accounts/" + account + "/trainingRuns/" + parts[1]}); err != nil {
		return "", err
	}
	return "accounts/" + account + "/trainingSamplers/checkpoints/" + checkpoint, nil
}

// CreateStandaloneBaseSamplingClient binds base weights without creating a run.
func (c *FiretitanServiceClient) CreateStandaloneBaseSamplingClient(ctx context.Context, baseModel string, tokenizer DeploymentTokenizer, controller SamplingConcurrencyController) (*FiretitanSamplingClient, error) {
	if !strings.HasSuffix(c.ManagedBaseURL, "/training/v1/serverless") {
		return nil, fmt.Errorf("standalone sampling requires a serverless training endpoint")
	}
	if strings.TrimSpace(baseModel) == "" {
		return nil, fmt.Errorf("base_model is required")
	}
	account := c.ResolvedAccountID(ctx)
	if account == "" {
		return nil, fmt.Errorf("standalone sampling requires a resolvable account")
	}
	if err := c.bindStandaloneSamplerRoute(ctx, map[string]any{"base_model": baseModel}); err != nil {
		return nil, err
	}
	return c.newServerlessSamplingClient("accounts/"+account+"/trainingSamplers/"+baseModel, tokenizer, controller)
}

func (c *FiretitanServiceClient) FetchServerCapabilities(ctx context.Context) (ServerCapabilitiesResponse, error) {
	if c == nil || isServerlessGatewayURL(c.ManagedBaseURL) {
		return ServerCapabilitiesResponse{}, fmt.Errorf("server-wide capabilities are unavailable here; use the selected model's creation capabilities")
	}
	endpoint := ""
	if c.ProvisionedHandle != nil {
		endpoint = c.ProvisionedHandle.TrainerEndpoint.BaseURL
	}
	if endpoint == "" {
		return ServerCapabilitiesResponse{}, fmt.Errorf("server capabilities require a provisioned dedicated trainer")
	}
	return NewNativeTrainingRuntime(NewTrainingRestClient(c.Fireworks.APIKey(), endpoint), "").GetServerCapabilities(ctx)
}
