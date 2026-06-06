package sdk

import (
	"context"
	"errors"
)

var ErrNilFuture = errors.New("Future is nil")

type Future[T any] struct {
	done   chan struct{}
	result T
	err    error
}

func SubmitFuture[T any](fn func() (T, error)) *Future[T] {
	future := &Future[T]{done: make(chan struct{})}
	go func() {
		defer close(future.done)
		future.result, future.err = fn()
	}()
	return future
}

func ReadyFuture[T any](result T, err error) *Future[T] {
	future := &Future[T]{
		done:   make(chan struct{}),
		result: result,
		err:    err,
	}
	close(future.done)
	return future
}

func (f *Future[T]) Result(ctx context.Context) (T, error) {
	var zero T
	if f == nil {
		return zero, ErrNilFuture
	}
	if ctx == nil {
		<-f.done
		return f.result, f.err
	}
	select {
	case <-f.done:
		return f.result, f.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func (f *Future[T]) Await() (T, error) {
	return f.Result(context.Background())
}

func (f *Future[T]) Ready() bool {
	if f == nil {
		return false
	}
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

func (f *Future[T]) Done() <-chan struct{} {
	if f == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return f.done
}
