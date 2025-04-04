package sink

import (
	"context"
	"fmt"
)

// A Mutex is a mutual exclusion lock that guards a specific value. The zero
// value of a Mutex is invalid. It is safe to copy a Mutex.
type Mutex[T any] struct {
	// A single-element buffer that holds the guarded value; receiving the
	// element is equivalent to Lock() and replacing it is equivalent to
	// Unlock().
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

type closedErr[T any, M interface {
	Mutex[T] | Monitor[T] | PriorityMutex[T]
}] struct{}

func (closedErr[T, M]) Error() string {
	return fmt.Sprintf("%T closed", *(new(M)))
}

type (
	// An ExclusiveAccess function receives a guarded value with a guarantee of
	// mutual exclusion. Any error returned by an ExclusiveAccess function is
	// propagated.
	ExclusiveAccess[T any] func(T) error
	// A PreemptibleExclusiveAccess function is equivalent to an
	// [ExclusiveAccess] function except that it SHOULD yield the guarded value
	// when receiving on the [Priority] channel if the received value is higher
	// than its own.
	PreemptibleExclusiveAccess[T any] func(preempt <-chan Priority, v T) error
	// An ExclusiveAccessValuer function is equivalent to an [ExclusiveAccess]
	// function except that it returns a value in addition to an error. T and U
	// MAY be the same type.
	ExclusiveAccessValuer[T any, U any]             func(T) (U, error)
	ExclusiveMultiAccessValuer[T any, U any, V any] func(T, U) (V, error)
)

// Use calls `fn` with the guarded value. It is the equivalent of locking and
// then unlocking `mu`.
func (mu Mutex[T]) Use(ctx context.Context, fn ExclusiveAccess[T]) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case v, ok := <-mu.ch:
		if !ok {
			return closedErr[T, Mutex[T]]{}
		}
		err := fn(v)
		mu.ch <- v
		return err
	}
}

// Replace calls `fn` with the guarded value and replaces it with the value
// returned by `fn`. It is otherwise equivalent to [Mutex.Use].
func (mu Mutex[T]) Replace(ctx context.Context, fn ExclusiveAccessValuer[T, T]) error {
	select {
	case <-ctx.Done():
		return ctx.Err()

	case v, ok := <-mu.ch:
		if !ok {
			return closedErr[T, Mutex[T]]{}
		}
		v, err := fn(v)
		mu.ch <- v
		return err
	}
}

// Close releases the Mutex's resources. Any future calls to [Mutex.Use] or
// [Mutex.Replace] will return an error. Close returns the guarded value.
func (mu Mutex[T]) Close() T {
	x := <-mu.ch
	close(mu.ch)
	return x
}

// FromMutex is a convenience wrapper around [Mutex.Use], returning a value
// derived from the guarded value, which is unchanged.
func FromMutex[T any, U any](ctx context.Context, mu Mutex[T], fn ExclusiveAccessValuer[T, U]) (U, error) {
	var u U
	err := mu.Use(ctx, func(t T) error {
		var err error
		u, err = fn(t)
		return err
	})
	return u, err
}
