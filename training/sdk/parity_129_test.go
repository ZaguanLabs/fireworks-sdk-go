package sdk

import "testing"

func TestTrainerReservationDefaultsToTrueAndCanOptOut(t *testing.T) {
	defaultPayload := BuildTrainerCreatePayload(TrainerJobConfig{BaseModel: "accounts/a/models/m"})
	if got, ok := defaultPayload["useReservation"].(bool); !ok || !got {
		t.Fatalf("default useReservation = %#v", defaultPayload["useReservation"])
	}

	useShared := false
	payload := BuildTrainerCreatePayload(TrainerJobConfig{
		BaseModel:      "accounts/a/models/m",
		UseReservation: &useShared,
	})
	if got, ok := payload["useReservation"].(bool); !ok || got {
		t.Fatalf("opt-out useReservation = %#v", payload["useReservation"])
	}
}

func TestManagedReservationAndCMEKParity(t *testing.T) {
	normalized, err := (FiretitanProvisioningConfig{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.UseReservation == nil || !*normalized.UseReservation {
		t.Fatalf("normalized reservation = %#v", normalized.UseReservation)
	}

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--cmek-output-model-resource", "models/key"}, "models/key"},
		{[]string{"--cmek-output-model-resource=models/key"}, "models/key"},
		{[]string{"--other", "value"}, ""},
	} {
		if got := PolicyOutputCMEKResource(test.args); got != test.want {
			t.Fatalf("PolicyOutputCMEKResource(%q) = %q, want %q", test.args, got, test.want)
		}
	}

	metadata := map[string]string{"fireworks_cmek_resource": "legacy", "recipe": "sft"}
	reference := ReferenceUserMetadata(metadata)
	if reference["fireworks_cmek_resource"] != "legacy" || reference["recipe"] != "sft" {
		t.Fatalf("reference metadata = %#v", reference)
	}
	reference["recipe"] = "changed"
	if metadata["recipe"] != "sft" {
		t.Fatal("reference metadata must be cloned")
	}
}
