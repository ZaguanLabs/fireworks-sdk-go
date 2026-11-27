package tito

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RolloutPhaseMetrics partitions elapsed time on one local monotonic clock.
// Work duration counts each operation independently, including overlapping work.
type RolloutPhaseMetrics struct {
	mu          sync.Mutex
	clock       func() time.Time
	start, last time.Time
	closed      bool
	active      [3]int
	work        [3]time.Duration
	wall        [5]time.Duration
	outcomes    [3][3]int
}

var phases = [...]string{"model", "tool", "other"}
var outcomes = [...]string{"succeeded", "failed", "cancelled"}
var wallPhases = [...]string{"unattributed", "model_only", "tool_only", "model_tool_overlap", "other"}

func NewRolloutPhaseMetrics() *RolloutPhaseMetrics { return newPhaseMetrics(time.Now) }

func newPhaseMetrics(clock func() time.Time) *RolloutPhaseMetrics {
	now := clock()
	return &RolloutPhaseMetrics{clock: clock, start: now, last: now}
}

func (m *RolloutPhaseMetrics) advance(now time.Time) {
	index := 0
	if m.active[0] > 0 {
		index++
	}
	if m.active[1] > 0 {
		index += 2
	}
	if index == 0 && m.active[2] > 0 {
		index = 4
	}
	m.wall[index] += now.Sub(m.last)
	m.last = now
}

func namedIndex(names []string, value string) int {
	for i, name := range names {
		if value == name {
			return i
		}
	}
	return -1
}

// Begin opens one model, tool, or other phase. The returned end function
// records its outcome and rejects accidental duplicate closure.
func (m *RolloutPhaseMetrics) Begin(phase string) (func(string) error, error) {
	i := namedIndex(phases[:], phase)
	if i < 0 {
		return nil, fmt.Errorf("unknown rollout phase %q", phase)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("rollout metrics already finished")
	}
	start := m.clock()
	m.advance(start)
	m.active[i]++
	ended := false
	return func(outcome string) error {
		j := namedIndex(outcomes[:], outcome)
		if j < 0 {
			return fmt.Errorf("unknown phase outcome %q", outcome)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if ended {
			return errors.New("phase already ended")
		}
		now := m.clock()
		m.advance(now)
		m.active[i]--
		m.work[i] += now.Sub(start)
		m.outcomes[i][j]++
		ended = true
		return nil
	}, nil
}

// Measure accounts for success, errors, cancellation and panic.
func (m *RolloutPhaseMetrics) Measure(phase string, fn func() error) (err error) {
	end, err := m.Begin(phase)
	if err != nil {
		return err
	}
	outcome := "failed"
	defer func() { _ = end(outcome) }()
	err = fn()
	if err == nil {
		outcome = "succeeded"
	} else if errors.Is(err, context.Canceled) {
		outcome = "cancelled"
	}
	return err
}

func (m *RolloutPhaseMetrics) Finish() (map[string]float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != ([3]int{}) {
		return nil, errors.New("rollout phases still active")
	}
	if !m.closed {
		m.advance(m.clock())
		m.closed = true
	}
	result := map[string]float64{"wall_seconds": m.last.Sub(m.start).Seconds()}
	for i, name := range wallPhases {
		result[name+"_wall_seconds"] = m.wall[i].Seconds()
	}
	for i, name := range phases {
		result[name+"_work_seconds"] = m.work[i].Seconds()
		for j, outcome := range outcomes {
			result[name+"/"+outcome] = float64(m.outcomes[i][j])
		}
	}
	return result, nil
}
