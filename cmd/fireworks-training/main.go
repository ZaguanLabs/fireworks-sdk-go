package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: fireworks-training <setup-trainer|setup-deployment> [flags]")
	}
	switch args[0] {
	case "setup-trainer":
		return runSetupTrainer(args[1:])
	case "setup-deployment":
		return runSetupDeployment(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runSetupTrainer(args []string) error {
	fs := flag.NewFlagSet("setup-trainer", flag.ContinueOnError)
	displayName := fs.String("display-name", "", "Display name for the trainer job")
	baseModel := fs.String("base-model", sdk.DefaultSetupBaseModel, "Base model")
	customImageTag := fs.String("custom-image-tag", "", "Custom trainer image tag")
	region := fs.String("region", sdk.DefaultSetupRegion, "Region")
	nodeCount := fs.Int("node-count", 2, "Trainer node count")
	extraArgs := fs.String("extra-args", strings.Join(sdk.DefaultSetupTrainerExtraArgs, " "), "Space-separated extra trainer args")
	timeoutS := fs.Float64("timeout-s", 1200, "Timeout in seconds")
	pollIntervalS := fs.Float64("poll-interval-s", 5, "Poll interval in seconds")
	loraRank := fs.Int("lora-rank", 0, "LoRA rank")
	maxSeqLen := fs.Int("max-seq-len", 4096, "Max sequence length")
	acceleratorType := fs.String("accelerator-type", "", "Accelerator type")
	acceleratorCount := fs.Int("accelerator-count", 0, "Accelerator count")
	apiKey := fs.String("fireworks-api-key", "", "Fireworks API key")
	baseURL := fs.String("fireworks-base-url", "", "Fireworks base URL")
	additionalHeadersRaw := fs.String("additional-headers", "", "JSON object of additional headers")
	outputFile := fs.String("output-file", sdk.DefaultSetupTrainerOutput, "Output JSON file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	additionalHeaders, err := sdk.ParseAdditionalHeadersJSON(*additionalHeadersRaw)
	if err != nil {
		return fmt.Errorf("parse --additional-headers: %w", err)
	}
	var acceleratorCountPtr *int
	if *acceleratorCount != 0 {
		acceleratorCountPtr = acceleratorCount
	}
	output, err := sdk.SetupTrainer(context.Background(), sdk.SetupTrainerOptions{
		DisplayName:       *displayName,
		BaseModel:         *baseModel,
		CustomImageTag:    *customImageTag,
		Region:            *region,
		NodeCount:         *nodeCount,
		ExtraArgs:         sdk.SplitSetupExtraArgs(*extraArgs),
		Timeout:           secondsDuration(*timeoutS),
		PollInterval:      secondsDuration(*pollIntervalS),
		LoraRank:          *loraRank,
		MaxSeqLen:         *maxSeqLen,
		AcceleratorType:   *acceleratorType,
		AcceleratorCount:  acceleratorCountPtr,
		FireworksAPIKey:   *apiKey,
		FireworksBaseURL:  *baseURL,
		AdditionalHeaders: additionalHeaders,
		OutputFile:        *outputFile,
	})
	if err != nil {
		return fmt.Errorf("TRAINER_FAILED: %w", err)
	}
	return writeStdoutJSON(output)
}

func runSetupDeployment(args []string) error {
	fs := flag.NewFlagSet("setup-deployment", flag.ContinueOnError)
	deploymentID := fs.String("deployment-id", "", "Deployment ID to create")
	deploymentShape := fs.String("deployment-shape", "", "Deployment shape")
	baseModel := fs.String("base-model", sdk.DefaultSetupBaseModel, "Base model")
	region := fs.String("region", sdk.DefaultSetupRegion, "Region")
	timeoutS := fs.Float64("timeout-s", 1800, "Timeout in seconds")
	apiKey := fs.String("fireworks-api-key", "", "Fireworks API key")
	baseURL := fs.String("fireworks-base-url", "", "Fireworks base URL")
	additionalHeadersRaw := fs.String("additional-headers", "", "JSON object of additional headers")
	hotloadAPIURL := fs.String("hotload-api-url", sdk.DefaultFireworksAPIURL, "Hotload API URL")
	outputFile := fs.String("output-file", sdk.DefaultSetupDeploymentOutput, "Output JSON file")
	skipShapeValidation := fs.Bool("skip-shape-validation", false, "Skip deployment shape validation")
	acceleratorType := fs.String("accelerator-type", "", "Accelerator type")
	if err := fs.Parse(args); err != nil {
		return err
	}
	additionalHeaders, err := sdk.ParseAdditionalHeadersJSON(*additionalHeadersRaw)
	if err != nil {
		return fmt.Errorf("parse --additional-headers: %w", err)
	}
	output, err := sdk.SetupDeployment(context.Background(), sdk.SetupDeploymentOptions{
		DeploymentID:        *deploymentID,
		DeploymentShape:     *deploymentShape,
		BaseModel:           *baseModel,
		Region:              *region,
		Timeout:             secondsDuration(*timeoutS),
		FireworksAPIKey:     *apiKey,
		FireworksBaseURL:    *baseURL,
		AdditionalHeaders:   additionalHeaders,
		HotloadAPIURL:       *hotloadAPIURL,
		OutputFile:          *outputFile,
		SkipShapeValidation: *skipShapeValidation,
		AcceleratorType:     *acceleratorType,
	})
	if err != nil {
		return fmt.Errorf("DEPLOYMENT_FAILED: %w", err)
	}
	return writeStdoutJSON(output)
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

func writeStdoutJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
