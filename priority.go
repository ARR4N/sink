package sink

import (
	"context"
	"math"
)

// A PriorityMutex is equivalent to a [Mutex] except that it is possible preempt
// the current holder of the lock. See [PriorityMutex.Use] for details.
type PriorityMutex[T any] struct {
	ch      chan T
	preempt chan Priority
}

// NewPriorityMutex creates a new PriorityMutex and sets the initial value to
// `init`. Call [PriorityMutex.Close] to release resources.
func NewPriorityMutex[T any](init T) PriorityMutex[T] {
	mu := PriorityMutex[T]{
		ch:      make(chan T, 1),
		preempt: make(chan Priority),
	}
	mu.ch <- init
	return mu
}

// A Priority indicates the relative priority of a call to [PriorityMutex.Use].
// Idiomatic usage is to treat higher values as being of greater importance;
// e.g. 0 for long-running background tasks and [math.MaxUint] for a
// time-critical preemption.
type Priority uint

// Idiomatic [Priority] bounds.
const (
	MinPriority = Priority(0)
	MaxPriority = Priority(math.MaxUint)
)

// Use calls `fn` with the guarded value. It is the equivalent of locking and
// then unlocking `mu`. The implementation of `fn` SHOULD receive values sent on
// the [Priority] channel that it takes as an argument, and return early if a
// higher-priority use is attempted, akin to honouring [context.Context]
// cancellation.
func (mu PriorityMutex[T]) Use(ctx context.Context, priority Priority, fn PreemptibleExclusiveAccess[T]) error {
	for {
		select {
		case mu.preempt <- priority:
			_ = 0 //

		case x, ok := <-mu.ch:
			if !ok {
				return closedErr[T, PriorityMutex[T]]{}
			}
			err := fn(mu.preempt, x)
			mu.ch <- x
			return err

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Close releases the PriorityMutex's resources. Any future calls to
// [PriorityMutex.Use] will return an error. Close returns the guarded value.
func (mu PriorityMutex[T]) Close() T {
	x := <-mu.ch
	close(mu.ch)
	return x
}

// FromPriorityMutex is a convenience wrapper around [PriorityMutex.Use],
// returning a value derived from the guarded value, which is unchanged.
func FromPriorityMutex[T any, U any](ctx context.Context, mu PriorityMutex[T], priority Priority, fn PreemptibleExclusiveAccessValuer[T, U]) (U, error) {
	var u U
	err := mu.Use(ctx, priority, func(preempt <-chan Priority, v T) error {
		var err error
		u, err = fn(preempt, v)
		return err
	})
	return u, err
}
