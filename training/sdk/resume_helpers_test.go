package sdk

import (
	"strings"
	"testing"
)

func TestTrainingClientPlanFromWeightsInfoBaseCheckpoint(t *testing.T) {
	plan, err := TrainingClientPlanFromWeightsInfo(WeightsInfo{
		BaseModel: " accounts/acct/models/base ",
		IsLora:    false,
		LoraRank:  intPointer(16),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.BaseModel != "accounts/acct/models/base" {
		t.Fatalf("base model = %q", plan.BaseModel)
	}
	if plan.IsLora || plan.LoraRank != 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.TrainUnembed || !plan.TrainMLP || !plan.TrainAttn {
		t.Fatalf("base checkpoint train flags should default true: %#v", plan)
	}
}

func TestTrainingClientPlanFromWeightsInfoLoraDefaultsTrainFlags(t *testing.T) {
	plan, err := TrainingClientPlanFromWeightsInfo(WeightsInfo{
		BaseModel: "accounts/acct/models/base",
		IsLora:    true,
		LoraRank:  intPointer(8),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.IsLora || plan.LoraRank != 8 {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.TrainUnembed || !plan.TrainMLP || !plan.TrainAttn {
		t.Fatalf("LoRA train flags should default true: %#v", plan)
	}
}

func TestTrainingClientPlanFromWeightsInfoLoraExplicitTrainFlags(t *testing.T) {
	plan, err := TrainingClientPlanFromWeightsInfo(WeightsInfo{
		BaseModel:    "accounts/acct/models/base",
		IsLora:       true,
		LoraRank:     intPointer(4),
		TrainUnembed: boolPointer(false),
		TrainMLP:     boolPointer(true),
		TrainAttn:    boolPointer(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.TrainUnembed || !plan.TrainMLP || plan.TrainAttn {
		t.Fatalf("explicit train flags not preserved: %#v", plan)
	}
}

func TestTrainingClientPlanFromWeightsInfoLoraRequiresRank(t *testing.T) {
	_, err := TrainingClientPlanFromWeightsInfo(WeightsInfo{
		BaseModel: "accounts/acct/models/base",
		IsLora:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "lora_rank") {
		t.Fatalf("error = %v", err)
	}
}

func TestTrainingClientPlanFromWeightsInfoRequiresBaseModel(t *testing.T) {
	_, err := TrainingClientPlanFromWeightsInfo(WeightsInfo{IsLora: true, LoraRank: intPointer(8)})
	if err == nil || !strings.Contains(err.Error(), "base_model") {
		t.Fatalf("error = %v", err)
	}
}

func TestRejectServiceWeightsAccessToken(t *testing.T) {
	if err := RejectServiceWeightsAccessToken("create_training_client_from_state", nil); err != nil {
		t.Fatal(err)
	}
	emptyToken := ""
	if err := RejectServiceWeightsAccessToken("create_training_client_from_state", &emptyToken); err == nil {
		t.Fatal("expected empty provided token to be rejected")
	}
	token := "token"
	err := RejectServiceWeightsAccessToken("create_training_client_from_state", &token)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "FiretitanServiceClient.create_training_client_from_state(weights_access_token=...)") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "current Fireworks API key") {
		t.Fatalf("error = %v", err)
	}
}

func TestRejectTrainingLoadWeightsAccessToken(t *testing.T) {
	if err := RejectTrainingLoadWeightsAccessToken("load_state", nil); err != nil {
		t.Fatal(err)
	}
	token := "token"
	err := RejectTrainingLoadWeightsAccessToken("load_state_with_optimizer", &token)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "FiretitanTrainingClient.load_state_with_optimizer(weights_access_token=...)") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSaveStateOptionsRejectsOverwrite(t *testing.T) {
	if err := ValidateSaveStateOptions(false); err != nil {
		t.Fatal(err)
	}
	err := ValidateSaveStateOptions(true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "save_state(overwrite=True)") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "Use a new checkpoint name") {
		t.Fatalf("error = %v", err)
	}
}
