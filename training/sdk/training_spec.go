package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

type ScheduleType string

const (
	ScheduleTypeConstant ScheduleType = "constant"
	ScheduleTypeLinear   ScheduleType = "linear"
	ScheduleTypeCosine   ScheduleType = "cosine"
	ScheduleTypeWSD      ScheduleType = "wsd"
)

type WSDDecayType string

const (
	WSDDecayTypeLinear WSDDecayType = "linear"
	WSDDecayTypeCosine WSDDecayType = "cosine"
	WSDDecayTypeSqrt   WSDDecayType = "sqrt"
)

type LRSchedulerSpec interface {
	schedulerType() ScheduleType
	warmupSpec() (steps int, ratio *float64)
	validate() error
}

type ScheduleBase struct {
	WarmupSteps int      `json:"warmup_steps,omitempty"`
	WarmupRatio *float64 `json:"warmup_ratio,omitempty"`
}

type ConstantSchedule struct {
	ScheduleBase
	Type ScheduleType `json:"type"`
}

type LinearSchedule struct {
	ScheduleBase
	Type       ScheduleType `json:"type"`
	DecayRatio *float64     `json:"decay_ratio,omitempty"`
	MinLRRatio float64      `json:"min_lr_ratio,omitempty"`
}

type CosineSchedule struct {
	ScheduleBase
	Type       ScheduleType `json:"type"`
	DecayRatio *float64     `json:"decay_ratio,omitempty"`
	MinLRRatio float64      `json:"min_lr_ratio,omitempty"`
}

type WSDSchedule struct {
	ScheduleBase
	Type       ScheduleType `json:"type"`
	DecayRatio float64      `json:"decay_ratio,omitempty"`
	DecayType  WSDDecayType `json:"decay_type,omitempty"`
	MinLRRatio float64      `json:"min_lr_ratio,omitempty"`
}

type NormalizeLRSchedulerOptions struct {
	LegacyLRSchedule  string
	LegacyWarmupSteps *int
	LegacyWarmupRatio *float64
	LegacyMinLRRatio  *float64
}

// StrictSpec mirrors the Python SDK marker type that rejects unknown fields for
// scheduler payload validation. It is intentionally empty in Go because strict
// decoding behavior is enforced by decodeStrict.
type StrictSpec struct{}

func DefaultConstantSchedule() ConstantSchedule {
	return ConstantSchedule{Type: ScheduleTypeConstant}
}

func (s ConstantSchedule) schedulerType() ScheduleType {
	if s.Type == "" {
		return ScheduleTypeConstant
	}
	return s.Type
}

func (s ConstantSchedule) warmupSpec() (int, *float64) {
	return s.WarmupSteps, s.WarmupRatio
}

func (s ConstantSchedule) validate() error {
	if s.schedulerType() != ScheduleTypeConstant {
		return fmt.Errorf("constant schedule type must be %q", ScheduleTypeConstant)
	}
	return validateWarmup(s.ScheduleBase)
}

func (s LinearSchedule) schedulerType() ScheduleType {
	if s.Type == "" {
		return ScheduleTypeLinear
	}
	return s.Type
}

func (s LinearSchedule) warmupSpec() (int, *float64) {
	return s.WarmupSteps, s.WarmupRatio
}

func (s LinearSchedule) validate() error {
	if s.schedulerType() != ScheduleTypeLinear {
		return fmt.Errorf("linear schedule type must be %q", ScheduleTypeLinear)
	}
	if err := validateWarmup(s.ScheduleBase); err != nil {
		return err
	}
	if s.DecayRatio != nil && (*s.DecayRatio < 0 || *s.DecayRatio > 1) {
		return fmt.Errorf("decay_ratio must be between 0 and 1")
	}
	return validateMinLRRatio(s.MinLRRatio)
}

func (s CosineSchedule) schedulerType() ScheduleType {
	if s.Type == "" {
		return ScheduleTypeCosine
	}
	return s.Type
}

func (s CosineSchedule) warmupSpec() (int, *float64) {
	return s.WarmupSteps, s.WarmupRatio
}

func (s CosineSchedule) validate() error {
	if s.schedulerType() != ScheduleTypeCosine {
		return fmt.Errorf("cosine schedule type must be %q", ScheduleTypeCosine)
	}
	if err := validateWarmup(s.ScheduleBase); err != nil {
		return err
	}
	if s.DecayRatio != nil && (*s.DecayRatio < 0 || *s.DecayRatio > 1) {
		return fmt.Errorf("decay_ratio must be between 0 and 1")
	}
	return validateMinLRRatio(s.MinLRRatio)
}

func (s WSDSchedule) schedulerType() ScheduleType {
	if s.Type == "" {
		return ScheduleTypeWSD
	}
	return s.Type
}

func (s WSDSchedule) warmupSpec() (int, *float64) {
	return s.WarmupSteps, s.WarmupRatio
}

func (s WSDSchedule) validate() error {
	if s.schedulerType() != ScheduleTypeWSD {
		return fmt.Errorf("wsd schedule type must be %q", ScheduleTypeWSD)
	}
	if err := validateWarmup(s.ScheduleBase); err != nil {
		return err
	}
	decayRatio := s.DecayRatio
	if decayRatio == 0 {
		decayRatio = 0.1
	}
	if decayRatio <= 0 || decayRatio > 1 {
		return fmt.Errorf("decay_ratio must be greater than 0 and less than or equal to 1")
	}
	decayType := s.DecayType
	if decayType == "" {
		decayType = WSDDecayTypeLinear
	}
	switch decayType {
	case WSDDecayTypeLinear, WSDDecayTypeCosine, WSDDecayTypeSqrt:
	default:
		return fmt.Errorf("unknown WSD decay_type: %s", decayType)
	}
	return validateMinLRRatio(s.MinLRRatio)
}

func ParseLRSchedulerSpec(data any) (LRSchedulerSpec, error) {
	switch v := data.(type) {
	case nil:
		return nil, fmt.Errorf("lr scheduler spec is required")
	case LRSchedulerSpec:
		if err := v.validate(); err != nil {
			return nil, err
		}
		return v, nil
	case []byte:
		return parseLRSchedulerJSON(v)
	case string:
		return parseLRSchedulerJSON([]byte(v))
	default:
		payload, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return parseLRSchedulerJSON(payload)
	}
}

func NormalizeLRSchedulerSpec(data any, opts NormalizeLRSchedulerOptions) (LRSchedulerSpec, error) {
	var spec LRSchedulerSpec
	var err error
	if data == nil {
		spec = DefaultConstantSchedule()
	} else {
		spec, err = ParseLRSchedulerSpec(data)
		if err != nil {
			return nil, err
		}
	}

	hasLegacy := intValue(opts.LegacyWarmupSteps) > 0 ||
		floatValue(opts.LegacyWarmupRatio) > 0 ||
		(opts.LegacyLRSchedule != "" && opts.LegacyLRSchedule != string(ScheduleTypeConstant)) ||
		floatValue(opts.LegacyMinLRRatio) > 0
	if !hasLegacy {
		return spec, nil
	}
	if data != nil && !isDefaultConstantSchedule(spec) {
		return spec, nil
	}

	scheduleType := opts.LegacyLRSchedule
	if scheduleType == "" {
		scheduleType = string(spec.schedulerType())
	}
	payload := map[string]any{"type": scheduleType}
	if steps := intValue(opts.LegacyWarmupSteps); steps > 0 {
		payload["warmup_steps"] = steps
	} else if ratio := floatValue(opts.LegacyWarmupRatio); ratio > 0 {
		payload["warmup_ratio"] = ratio
	}
	if scheduleType != string(ScheduleTypeConstant) && opts.LegacyMinLRRatio != nil {
		payload["min_lr_ratio"] = *opts.LegacyMinLRRatio
	}
	return ParseLRSchedulerSpec(payload)
}

func HasV1SchedulerFields(fields []string) bool {
	for _, field := range fields {
		switch field {
		case "lr_schedule", "lr_warmup_steps", "warmup_steps", "warmup_ratio", "min_lr_ratio":
			return true
		}
	}
	return false
}

func ComputeLR(spec LRSchedulerSpec, step int, baseLR float64, totalSteps ...int) (float64, error) {
	if spec == nil {
		return 0, fmt.Errorf("lr scheduler spec is required")
	}
	if err := spec.validate(); err != nil {
		return 0, err
	}
	total := 0
	if len(totalSteps) > 0 {
		total = totalSteps[0]
	}
	warmup, err := resolveWarmupSteps(spec, total)
	if err != nil {
		return 0, err
	}
	if warmup > 0 && step <= warmup {
		return baseLR * (float64(step) / float64(warmup)), nil
	}

	switch s := spec.(type) {
	case ConstantSchedule, *ConstantSchedule:
		return baseLR, nil
	case LinearSchedule:
		return computeDecayLR(s.DecayRatio, s.MinLRRatio, ScheduleTypeLinear, step, baseLR, total, warmup)
	case *LinearSchedule:
		return computeDecayLR(s.DecayRatio, s.MinLRRatio, ScheduleTypeLinear, step, baseLR, total, warmup)
	case CosineSchedule:
		return computeDecayLR(s.DecayRatio, s.MinLRRatio, ScheduleTypeCosine, step, baseLR, total, warmup)
	case *CosineSchedule:
		return computeDecayLR(s.DecayRatio, s.MinLRRatio, ScheduleTypeCosine, step, baseLR, total, warmup)
	case WSDSchedule:
		return computeWSDLR(s, step, baseLR, total, warmup)
	case *WSDSchedule:
		return computeWSDLR(*s, step, baseLR, total, warmup)
	default:
		return 0, fmt.Errorf("unsupported LRSchedulerSpec variant: %T", spec)
	}
}

func parseLRSchedulerJSON(payload []byte) (LRSchedulerSpec, error) {
	var discriminator struct {
		Type ScheduleType `json:"type"`
	}
	if err := json.Unmarshal(payload, &discriminator); err != nil {
		return nil, err
	}
	switch discriminator.Type {
	case ScheduleTypeConstant:
		var spec ConstantSchedule
		if err := decodeStrict(payload, &spec); err != nil {
			return nil, err
		}
		if spec.Type == "" {
			spec.Type = ScheduleTypeConstant
		}
		return spec, spec.validate()
	case ScheduleTypeLinear:
		var spec LinearSchedule
		if err := decodeStrict(payload, &spec); err != nil {
			return nil, err
		}
		if spec.Type == "" {
			spec.Type = ScheduleTypeLinear
		}
		return spec, spec.validate()
	case ScheduleTypeCosine:
		var spec CosineSchedule
		if err := decodeStrict(payload, &spec); err != nil {
			return nil, err
		}
		if spec.Type == "" {
			spec.Type = ScheduleTypeCosine
		}
		return spec, spec.validate()
	case ScheduleTypeWSD:
		var spec WSDSchedule
		if err := decodeStrict(payload, &spec); err != nil {
			return nil, err
		}
		if spec.Type == "" {
			spec.Type = ScheduleTypeWSD
		}
		if spec.DecayRatio == 0 {
			spec.DecayRatio = 0.1
		}
		if spec.DecayType == "" {
			spec.DecayType = WSDDecayTypeLinear
		}
		return spec, spec.validate()
	default:
		return nil, fmt.Errorf("unknown lr scheduler type: %q", discriminator.Type)
	}
}

func decodeStrict(payload []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func validateWarmup(base ScheduleBase) error {
	if base.WarmupSteps < 0 {
		return fmt.Errorf("warmup_steps must be greater than or equal to 0")
	}
	if base.WarmupRatio != nil && (*base.WarmupRatio < 0 || *base.WarmupRatio >= 1) {
		return fmt.Errorf("warmup_ratio must be greater than or equal to 0 and less than 1")
	}
	if base.WarmupSteps != 0 && base.WarmupRatio != nil {
		return fmt.Errorf("warmup_steps and warmup_ratio are mutually exclusive")
	}
	return nil
}

func validateMinLRRatio(value float64) error {
	if value < 0 || value > 1 {
		return fmt.Errorf("min_lr_ratio must be between 0 and 1")
	}
	return nil
}

func resolveWarmupSteps(spec LRSchedulerSpec, totalSteps int) (int, error) {
	steps, ratio := spec.warmupSpec()
	if steps != 0 {
		return steps, nil
	}
	if ratio == nil {
		return 0, nil
	}
	if totalSteps <= 0 {
		return 0, fmt.Errorf("warmup_ratio requires a positive total_steps; got total_steps=%d", totalSteps)
	}
	return max(0, int(math.Round(*ratio*float64(totalSteps)))), nil
}

func computeDecayLR(decayRatio *float64, minRatio float64, scheduleType ScheduleType, step int, baseLR float64, totalSteps int, warmup int) (float64, error) {
	if totalSteps <= 0 {
		return 0, fmt.Errorf("%s schedule requires total_steps; got total_steps=%d", scheduleType, totalSteps)
	}
	decayStart, decayEnd := decayBounds(decayRatio, totalSteps, warmup)
	if step <= decayStart {
		return baseLR, nil
	}
	if step >= decayEnd {
		return baseLR * minRatio, nil
	}
	progress := float64(step-decayStart) / float64(max(1, decayEnd-decayStart))
	if scheduleType == ScheduleTypeLinear {
		return baseLR * (1.0 - progress*(1.0-minRatio)), nil
	}
	cosine := 0.5 * (1.0 + math.Cos(math.Pi*progress))
	return baseLR * (minRatio + (1.0-minRatio)*cosine), nil
}

func computeWSDLR(spec WSDSchedule, step int, baseLR float64, totalSteps int, warmup int) (float64, error) {
	if totalSteps <= 0 {
		return 0, fmt.Errorf("WSDSchedule requires total_steps; got total_steps=%d", totalSteps)
	}
	decayRatio := spec.DecayRatio
	if decayRatio == 0 {
		decayRatio = 0.1
	}
	decayStart, decayEnd := decayBounds(&decayRatio, totalSteps, warmup)
	if step <= decayStart {
		return baseLR, nil
	}
	if step >= decayEnd {
		return baseLR * spec.MinLRRatio, nil
	}
	progress := float64(step-decayStart) / float64(max(1, decayEnd-decayStart))
	switch decayType := spec.DecayType; decayType {
	case "", WSDDecayTypeLinear:
		return baseLR * (1.0 - progress*(1.0-spec.MinLRRatio)), nil
	case WSDDecayTypeCosine:
		cosine := 0.5 * (1.0 + math.Cos(math.Pi*progress))
		return baseLR * (spec.MinLRRatio + (1.0-spec.MinLRRatio)*cosine), nil
	case WSDDecayTypeSqrt:
		return baseLR * math.Max(spec.MinLRRatio, 1.0-math.Sqrt(progress)*(1.0-spec.MinLRRatio)), nil
	default:
		return 0, fmt.Errorf("unknown WSD decay_type: %s", decayType)
	}
}

func decayBounds(decayRatio *float64, totalSteps int, warmup int) (int, int) {
	if decayRatio == nil {
		return warmup, totalSteps
	}
	decayWindow := max(1, int(math.Round(*decayRatio*float64(totalSteps))))
	return max(warmup, totalSteps-decayWindow), totalSteps
}

func isDefaultConstantSchedule(spec LRSchedulerSpec) bool {
	s, ok := spec.(ConstantSchedule)
	if !ok {
		if ptr, ptrOK := spec.(*ConstantSchedule); ptrOK && ptr != nil {
			s = *ptr
			ok = true
		}
	}
	if !ok {
		return false
	}
	return s.WarmupSteps == 0 && s.WarmupRatio == nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func floatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
