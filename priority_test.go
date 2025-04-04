package sink

import (
	"context"
	"testing"
)

func TestPriorityMutext(t *testing.T) {
	ctx := context.Background()

	var i int
	mu := NewPriorityMutex(&i)

	const (
		low Priority = iota + 1
		high
	)

	using := make(chan struct{})
	var gotPreemption Priority
	go func() {
		err := mu.Use(ctx, low, func(preempt <-chan Priority, v *int) error {
			*v++
			close(using)
			gotPreemption = <-preempt
			return nil
		})
		if err != nil {
			t.Errorf("%T.Use(ctx, %d, ...) error %v", mu, low, err)
		}
	}()

	<-using
	err := mu.Use(ctx, high, func(preempt <-chan Priority, v *int) error {
		*v++
		return nil
	})
	if err != nil {
		t.Errorf("%T.Use(ctx, %d, ...) error %v", mu, high, err)
	}
	if got, want := gotPreemption, high; got != want {
		t.Errorf("%T.Use() with low priority preempted with %T = %[2]d; want %d", mu, got, want)
	}
	t.Run("Close", func(t *testing.T) {
		if got, want := *mu.Close(), 2; got != want {
			t.Errorf("%T.Close() got %d; want %d", mu, got, want)
		}

		err := mu.Use(ctx, 0, nil)
		if _, ok := err.(closedErr[*int, PriorityMutex[*int]]); !ok {
			t.Errorf("%T.Use() after Close() got err %T (%[2]v); want of type %T", mu, err, closedErr[*int, PriorityMutex[*int]]{})
		}
	})
}
