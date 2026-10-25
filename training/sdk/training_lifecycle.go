package sdk

import (
	"context"
	"fmt"
)

type operationContextKey struct{}

func (c *FiretitanTrainingClient) beginOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil {
		return ctx, func() {}, fmt.Errorf("training client is nil")
	}
	if ctx.Value(operationContextKey{}) == c {
		return ctx, func() {}, nil
	}
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.unloading {
		return ctx, func() {}, fmt.Errorf("training client is unloading; new work is closed")
	}
	c.pendingOperations.Add(1)
	return context.WithValue(ctx, operationContextKey{}, c), c.pendingOperations.Done, nil
}

func submitTrainingFuture[T any](c *FiretitanTrainingClient, ctx context.Context, fn func(context.Context) (T, error)) *Future[T] {
	accepted, done, err := c.beginOperation(ctx)
	if err != nil {
		var zero T
		return ReadyFuture(zero, err)
	}
	return SubmitFuture(func() (T, error) { defer done(); return fn(accepted) })
}

type TrainingUnloadBackend interface {
	UnloadModel(context.Context, string, int) (map[string]any, error)
}

// UnloadModelFuture closes admission immediately and drains accepted operations.
// Failed cleanup may be retried, while training admission remains closed.
func (c *FiretitanTrainingClient) UnloadModelFuture(ctx context.Context) *Future[map[string]any] {
	if c == nil {
		return ReadyFuture[map[string]any](nil, fmt.Errorf("training client is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(operationContextKey{}) == c {
		return ReadyFuture[map[string]any](nil, fmt.Errorf("cannot unload inside an active training operation"))
	}
	c.admissionMu.Lock()
	defer c.admissionMu.Unlock()
	if c.unloadFuture != nil && (!c.unloadFuture.Ready() || c.unloadFuture.err == nil) {
		return c.unloadFuture
	}
	c.unloading = true
	c.unloadFuture = SubmitFuture(func() (map[string]any, error) {
		drained := make(chan struct{})
		go func() { c.pendingOperations.Wait(); close(drained) }()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-drained:
		}
		backend, ok := c.ComputeBackend.(TrainingUnloadBackend)
		if !ok {
			return nil, fmt.Errorf("compute backend does not implement unload_model")
		}
		c.operationMu.Lock()
		defer c.operationMu.Unlock()
		c.RequestSeqID++
		return backend.UnloadModel(ctx, c.ModelID, c.RequestSeqID)
	})
	return c.unloadFuture
}

func (c *FiretitanTrainingClient) UnloadModel(ctx context.Context) (map[string]any, error) {
	return c.UnloadModelFuture(ctx).Result(ctx)
}

func (c *FiretitanTrainingClient) ForwardFuture(ctx context.Context, data []TrainingDatum, loss string, config map[string]any) *Future[ForwardBackwardOutput] {
	return submitTrainingFuture(c, ctx, func(ctx context.Context) (ForwardBackwardOutput, error) { return c.Forward(ctx, data, loss, config) })
}
