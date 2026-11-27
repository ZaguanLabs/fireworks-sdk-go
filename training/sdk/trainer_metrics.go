package sdk

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"regexp"
)

type TrainerMetricRecord struct {
	Step      int64              `json:"step"`
	Metrics   map[string]float64 `json:"metrics"`
	SessionID string             `json:"session_id,omitempty"`
}

var trainerJobResource = regexp.MustCompile(`^accounts/[a-z0-9-]+/rlorTrainerJobs/[a-z0-9-]+$`)

// LogRLORTrainerJobMetrics submits each batch once because a lost response may already have committed it.
func (c *FireworksClient) LogRLORTrainerJobMetrics(ctx context.Context, name string, records []TrainerMetricRecord) error {
	if !trainerJobResource.MatchString(name) {
		return fmt.Errorf("invalid trainer job name %q", name)
	}
	resp, err := c.requestWithRetry(ctx, http.MethodPost, "/v1/"+name+":logMetrics", map[string]any{"name": name, "records": records}, nil, HTTPReadTimeout, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return &NativeTrainingError{Status: resp.StatusCode, Message: ParseAPIErrorBody(body)}
	}
	return nil
}

func finiteMetric(number any) (float64, bool) {
	value := reflect.ValueOf(number)
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), true
	case reflect.Float32, reflect.Float64:
		f := value.Float()
		return f, !math.IsInf(f, 0) && !math.IsNaN(f)
	default:
		return 0, false
	}
}

// LogTrainingMetrics appends finite script metrics for customer-managed RLOR jobs.
func (c *FiretitanServiceClient) LogTrainingMetrics(ctx context.Context, record map[string]any) error {
	if c.TrainingSessionID() != "" || c.Config.ManagedBy != "" {
		return nil
	}
	step, ok := finiteMetric(record["step"])
	if !ok || step >= math.Exp2(63) || step < -math.Exp2(63) {
		return fmt.Errorf("a numeric training step is required")
	}
	metrics := map[string]float64{}
	for key, raw := range record {
		if key == "step" || key == "session_id" {
			continue
		}
		if number, valid := finiteMetric(raw); valid {
			metrics[key] = number
		}
	}
	if len(metrics) == 0 {
		return nil
	}
	if c.Fireworks == nil || c.Fireworks.APIKey() == "" {
		return fmt.Errorf("an API key is required to report training metrics")
	}
	job, err := c.TrainerJobID()
	if err != nil {
		return err
	}
	account, err := c.Fireworks.cachedAccountID(ctx, false)
	if err != nil {
		return err
	}
	id, _ := record["session_id"].(string)
	return c.Fireworks.LogRLORTrainerJobMetrics(ctx, "accounts/"+account+"/rlorTrainerJobs/"+job, []TrainerMetricRecord{{Step: int64(step), Metrics: metrics, SessionID: id}})
}
