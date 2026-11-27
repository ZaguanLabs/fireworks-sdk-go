package tito

import (
	"context"
	"regexp"
	"sync"
)

var publicTerminationTag = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

// Keep terminal intent independently observable while a sampler or observer is
// blocked. All transport writes settle before an artifact can be frozen.
type trajectoryAdmission struct {
	mu       sync.Mutex
	lifetime context.Context
	cancel   context.CancelFunc
	pending  sync.WaitGroup
	count    int
	intent   map[string]any
}

func newTrajectoryAdmission() *trajectoryAdmission {
	ctx, cancel := context.WithCancel(context.Background())
	return &trajectoryAdmission{lifetime: ctx, cancel: cancel}
}
func (a *trajectoryAdmission) closedLocked() *Error {
	if a.intent == nil {
		return nil
	}
	return &Error{Code: "tito_trajectory_closed", Status: 410, Message: "trajectory is closing or already closed", Context: cloneAnyMap(a.intent)}
}
func (a *trajectoryAdmission) enter(ctx context.Context) (context.Context, func(), *Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.closedLocked(); e != nil {
		return nil, nil, e
	}
	a.pending.Add(1)
	a.count++
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.lifetime, cancel)
	return child, func() {
		stop()
		cancel()
		a.mu.Lock()
		a.count--
		a.pending.Done()
		a.mu.Unlock()
	}, nil
}
func (a *trajectoryAdmission) beginClose(status TrajectoryStatus, reason string) *Error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.closedLocked(); e != nil {
		return e
	}
	if status == TrajectoryStatusCompleted && a.count > 0 {
		return &Error{Code: "tito_call_in_flight", Status: 409, Message: "cannot finish while a sidecar call is in flight"}
	}
	action := "finish"
	switch status {
	case TrajectoryStatusAbandoned:
		action = "abandon"
	case TrajectoryStatusFailed:
		action = "fail"
	}
	var tag any
	if reason != "" {
		if publicTerminationTag.MatchString(reason) {
			tag = reason
		} else {
			tag = "caller_supplied_reason"
		}
	}
	a.intent = map[string]any{"terminal_action": action, "requested_terminal_status": status, "terminal_status": status, "terminal_reason": tag, "terminal_started_at": nowSeconds(), "termination_phase": "closing"}
	a.cancel()
	return nil
}
func (a *trajectoryAdmission) finishClose(status TrajectoryStatus, at float64, cause *Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intent["terminal_status"] = status
	a.intent["terminal_at"] = at
	a.intent["termination_phase"] = "terminal"
	if cause != nil {
		a.intent["cause_error_code"] = cause.Code
		a.intent["cause_origin"] = cause.Origin
		a.intent["terminal_reason"] = cause.Code
	}
}
