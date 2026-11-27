package sdk

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var trainingSessionResource = regexp.MustCompile(`^accounts/([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)/trainingSessions/(ts-[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?)$`)

func (c *FireworksClient) validateSessionResource(ctx context.Context, name string) error {
	parts := trainingSessionResource.FindStringSubmatch(name)
	if parts == nil {
		return fmt.Errorf("invalid training session resource %q", name)
	}
	account, err := c.AccountID(ctx)
	if err != nil {
		return err
	}
	if parts[1] != account {
		return fmt.Errorf("training session belongs to a different account")
	}
	return nil
}

func readSessionResponse(resp *http.Response, name string) (map[string]any, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, &NativeTrainingError{Status: resp.StatusCode, Message: ParseAPIErrorBody(body)}
	}
	row, err := decodeJSONMap(resp.Body)
	if err != nil {
		return nil, err
	}
	if row["name"] != name {
		return nil, fmt.Errorf("training session response did not match requested resource")
	}
	return row, nil
}

// ReserveTrainingSession creates an unbound session. Persist the ID and ownership marker before submitting.
func (c *FireworksClient) ReserveTrainingSession(ctx context.Context, id, displayName string) (map[string]any, error) {
	if !trainingSessionResource.MatchString("accounts/a/trainingSessions/"+id) || strings.TrimSpace(displayName) == "" {
		return nil, fmt.Errorf("a valid session ID and a nonempty ownership marker are required")
	}
	account, err := c.AccountID(ctx)
	if err != nil {
		return nil, err
	}
	parent := "accounts/" + account + "/trainingSessions"
	name := parent + "/" + id
	if err := c.validateSessionResource(ctx, name); err != nil {
		return nil, err
	}
	resp, err := c.Post(ctx, "/v1/"+parent+"?training_session_id="+url.QueryEscape(id), map[string]string{"displayName": displayName, "baseModel": ""}, nil)
	if err != nil {
		return nil, err
	}
	row, err := readSessionResponse(resp, name)
	if err == nil && row["displayName"] != displayName {
		return nil, fmt.Errorf("reserved session ownership marker did not match")
	}
	return row, err
}

// GetTrainingSession returns nil only when the control plane responds with HTTP 404.
func (c *FireworksClient) GetTrainingSession(ctx context.Context, name string) (map[string]any, error) {
	if err := c.validateSessionResource(ctx, name); err != nil {
		return nil, err
	}
	resp, err := c.Get(ctx, "/v1/"+name, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil
	}
	return readSessionResponse(resp, name)
}

// OpenReservedTrainingSession verifies exclusive ownership before starting model counters at zero.
func (c *FireworksClient) OpenReservedTrainingSession(ctx context.Context, name, marker string) (*FiretitanServiceClient, error) {
	if strings.TrimSpace(marker) == "" || c.APIKey() == "" {
		return nil, fmt.Errorf("an API key and ownership marker are required")
	}
	row, err := c.GetTrainingSession(ctx, name)
	if err != nil {
		return nil, err
	}
	if row == nil || row["displayName"] != marker {
		return nil, fmt.Errorf("reserved training session missing or ownership marker does not match")
	}
	if (row["baseModel"] != nil && row["baseModel"] != "") || (row["state"] != nil && row["state"] != "TRAINING_SESSION_STATE_UNSPECIFIED") {
		return nil, fmt.Errorf("only a fresh unbound training session can start new counters")
	}
	parts := trainingSessionResource.FindStringSubmatch(name)
	endpoint := fireworksAPIRootURL(c.BaseURL()) + "/training/v1/serverless"
	service, err := NewFiretitanServiceClient(FiretitanServiceClientOptions{FireworksClient: c, BaseURL: endpoint, AdditionalHeaders: c.additionalHeaders, TrainingSessionID: parts[2]})
	if err != nil {
		return nil, err
	}
	service.AccountIDCache = parts[1]
	rest := NewTrainingRestClient(c.APIKey(), endpoint, WithTrainingAdditionalHeaders(c.additionalHeaders), WithTrainingHTTPClient(c.httpClient))
	service.reservedRuntime = NewNativeTrainingRuntime(rest, parts[2])
	service.startReservedHeartbeat(10 * time.Second)
	return service, nil
}

func adapterResource(value string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != 4 || parts[0] != "accounts" || parts[1] == "" || parts[2] != "models" || parts[3] == "" {
		return "", fmt.Errorf("adapter_path must be a Fireworks PEFT model resource: accounts/<account>/models/<model>")
	}
	return value, nil
}

func (c *FiretitanServiceClient) startReservedHeartbeat(period time.Duration) {
	ctx, stop := context.WithCancel(context.Background())
	c.stopHeartbeat = stop
	c.heartbeatDone = make(chan struct{})
	go func() {
		defer close(c.heartbeatDone)
		lastSuccess := time.Now()
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attempt, cancel := context.WithTimeout(ctx, period)
				r := c.reservedRuntime
				resp, err := r.REST.requestWithRetry(attempt, http.MethodPost, "/api/v1/session_heartbeat", map[string]string{"session_id": r.SessionID, "type": "session_heartbeat"}, nil, period, false)
				if err == nil {
					_, readErr := io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr == nil {
						lastSuccess = time.Now()
					}
				}
				cancel()
				if time.Since(lastSuccess) > 2*time.Minute && ctx.Err() == nil {
					log.Printf("Fireworks training session %s has missed heartbeats for %s", r.SessionID, time.Since(lastSuccess).Round(time.Second))
				}
			}
		}
	}()
}
