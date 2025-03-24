package sink

import (
	"context"
	"errors"
	"fmt"
)

// A Mutex is a mutual exclusion lock that guards a specific value. The zero
// value of a Mutex is invalid. It is safe to copy a Mutex.
type Mutex[T any] struct {
	ch chan T
}

// NewMutex creates a new Mutex and sets the initial value to `init`. Call
// [Mutex.Close] to release resources.
func NewMutex[T any](init T) Mutex[T] {
	mu := Mutex[T]{
		ch: make(chan T, 1),
	}
	mu.ch <- init
	return mu
}

// ErrMutexClosed is returned by calls to [Mutex.Use] and [Mutex.Replace] that
// occur after a call to [Mutex.Close].
var ErrMutexClosed = errors.New("mutex closed")

// Use calls `fn` with the guarded value. It is the equivalent of locking and
// then unlocking `mu`.
func (mu Mutex[T]) Use(ctx context.Context, fn func(T) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case v, ok := <-mu.ch:
		if !ok {
			return fmt.Errorf("%w", ErrMutexClosed)
		}
		err := fn(v)
		mu.ch <- v
		return err
	}
}

// Replace calls `fn` with the guarded value and replaces it with the value
// returned by `fn`. It is otherwise equivalent to [Mutex.Use].
func (mu Mutex[T]) Replace(ctx context.Context, fn func(T) (T, error)) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case v, ok := <-mu.ch:
		if !ok {
			return fmt.Errorf("%w", ErrMutexClosed)
		}
		v, err := fn(v)
		mu.ch <- v
		return err
	}
}

// Close releases the Mutex's resources. Any future calls to [Mutex.Use] or
// [Mutex.Replace] will return [ErrMutexClosed].
func (mu Mutex[T]) Close() T {
	x := <-mu.ch
	close(mu.ch)
	return x
}

// FromMutex is a convenience wrapper around [Mutex.Replace], returning a value
// derived from the guarded value, which is unchanged.
func FromMutex[T any, U any](ctx context.Context, mu Mutex[T], fn func(T) (U, error)) (U, error) {
	var u U
	err := mu.Use(ctx, func(t T) error {
		var err error
		u, err = fn(t)
		return err
	})
	return u, err
}
