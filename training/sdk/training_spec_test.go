package sdk

import (
	"math"
	"strings"
	"testing"
)

func TestConstantScheduleWarmup(t *testing.T) {
	sched := ConstantSchedule{
		Type:         ScheduleTypeConstant,
		ScheduleBase: ScheduleBase{WarmupSteps: 4},
	}

	assertApproxLR(t, sched, 1, 1e-4, 0, 2.5e-5)
	assertApproxLR(t, sched, 4, 1e-4, 0, 1e-4)
	assertApproxLR(t, sched, 5, 1e-4, 0, 1e-4)
}

func TestCosineScheduleDecaysToMinRatio(t *testing.T) {
	sched := CosineSchedule{
		Type:         ScheduleTypeCosine,
		ScheduleBase: ScheduleBase{WarmupSteps: 2},
		MinLRRatio:   0.1,
	}

	assertApproxLR(t, sched, 1, 1.0, 10, 0.5)
	assertApproxLR(t, sched, 2, 1.0, 10, 1.0)
	assertApproxLR(t, sched, 10, 1.0, 10, 0.1)
}

func TestLinearScheduleRequiresTotalSteps(t *testing.T) {
	_, err := ComputeLR(LinearSchedule{Type: ScheduleTypeLinear}, 1, 1.0)
	if err == nil || !strings.Contains(err.Error(), "requires total_steps") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseRejectsConstantMinLRRatio(t *testing.T) {
	_, err := ParseLRSchedulerSpec(map[string]any{
		"type":         "constant",
		"min_lr_ratio": 0.1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeLegacyCosineFields(t *testing.T) {
	warmupRatio := 0.2
	minLRRatio := 0.1
	sched, err := NormalizeLRSchedulerSpec(nil, NormalizeLRSchedulerOptions{
		LegacyLRSchedule:  "cosine",
		LegacyWarmupRatio: &warmupRatio,
		LegacyMinLRRatio:  &minLRRatio,
	})
	if err != nil {
		t.Fatal(err)
	}
	cosine, ok := sched.(CosineSchedule)
	if !ok {
		t.Fatalf("schedule type = %T", sched)
	}
	if math.Abs(*cosine.WarmupRatio-0.2) > 1e-12 {
		t.Fatalf("warmup ratio = %f", *cosine.WarmupRatio)
	}
	if math.Abs(cosine.MinLRRatio-0.1) > 1e-12 {
		t.Fatalf("min LR ratio = %f", cosine.MinLRRatio)
	}
}

func TestNormalizeNestedScheduleWinsOverLegacyFields(t *testing.T) {
	warmupSteps := 10
	sched, err := NormalizeLRSchedulerSpec(
		LinearSchedule{Type: ScheduleTypeLinear, MinLRRatio: 0.2},
		NormalizeLRSchedulerOptions{LegacyLRSchedule: "cosine", LegacyWarmupSteps: &warmupSteps},
	)
	if err != nil {
		t.Fatal(err)
	}
	linear, ok := sched.(LinearSchedule)
	if !ok {
		t.Fatalf("schedule type = %T", sched)
	}
	if linear.WarmupSteps != 0 || linear.MinLRRatio != 0.2 {
		t.Fatalf("schedule = %#v", linear)
	}
}

func TestWarmupRatioRequiresTotalSteps(t *testing.T) {
	ratio := 0.1
	_, err := ComputeLR(ConstantSchedule{
		Type:         ScheduleTypeConstant,
		ScheduleBase: ScheduleBase{WarmupRatio: &ratio},
	}, 1, 1.0)
	if err == nil || !strings.Contains(err.Error(), "warmup_ratio requires") {
		t.Fatalf("error = %v", err)
	}
}

func TestWSDScheduleSqrtDecay(t *testing.T) {
	sched := WSDSchedule{
		Type:       ScheduleTypeWSD,
		DecayRatio: 0.5,
		DecayType:  WSDDecayTypeSqrt,
		MinLRRatio: 0.25,
	}
	lr, err := ComputeLR(sched, 8, 1.0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if lr <= 0.25 || lr >= 1.0 {
		t.Fatalf("lr = %f", lr)
	}
}

func TestHasV1SchedulerFields(t *testing.T) {
	if !HasV1SchedulerFields([]string{"foo", "lr_schedule"}) {
		t.Fatal("expected legacy scheduler field")
	}
	if HasV1SchedulerFields([]string{"foo", "bar"}) {
		t.Fatal("unexpected legacy scheduler field")
	}
}

func TestParseRejectsMutuallyExclusiveWarmup(t *testing.T) {
	ratio := 0.1
	_, err := ParseLRSchedulerSpec(ConstantSchedule{
		Type:         ScheduleTypeConstant,
		ScheduleBase: ScheduleBase{WarmupSteps: 1, WarmupRatio: &ratio},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRejectsUnknownScheduleType(t *testing.T) {
	_, err := ParseLRSchedulerSpec(map[string]any{"type": "unknown"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func assertApproxLR(t *testing.T, spec LRSchedulerSpec, step int, baseLR float64, totalSteps int, want float64) {
	t.Helper()
	var (
		got float64
		err error
	)
	if totalSteps == 0 {
		got, err = ComputeLR(spec, step, baseLR)
	} else {
		got, err = ComputeLR(spec, step, baseLR, totalSteps)
	}
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("lr = %.12f, want %.12f", got, want)
	}
}
