package sink

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestMonitor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var zero int
	mon := NewMonitor(&zero)

	t.Run("condition_met_immediately", func(t *testing.T) {
		var condCalls int
		err := mon.Wait(ctx,
			func(*int) bool {
				condCalls++
				return true
			},
			func(got *int) error {
				if got, want := *got, 0; got != want {
					t.Errorf("got %d, want %d", got, want)
				}
				return nil
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := condCalls, 1; got != want {
			t.Errorf("got %d calls to cond(); want %d", got, want)
		}
	})

	t.Run("multiple_condition_checks", func(t *testing.T) {
		var (
			gotCondChecks []int
			gotFinal      int
		)

		waiting := make(chan struct{})
		done := make(chan struct{})
		const threshold = 10
		go func() {
			defer close(done)

			errSentinel := errors.New("all good")

			var err error
			gotFinal, err = FromMonitor(ctx, mon,
				func(i *int) bool {
					select {
					case <-waiting:
					default:
						close(waiting)
					}

					gotCondChecks = append(gotCondChecks, *i)
					return *i > threshold
				},
				func(i *int) (int, error) {
					return *i, errSentinel
				},
			)
			if !errors.Is(err, errSentinel) {
				t.Error(err)
			}
		}()

		<-waiting

		const (
			// We deliberately increment the value beyond the threshold to check
			// that the waiter isn't signalled after its condition is met.
			n    = 10
			incr = 3
		)
		for range n {
			err := mon.UseThenSignal(ctx, func(i *int) error {
				*i += incr
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}

		<-done

		if diff := cmp.Diff([]int{0, 3, 6, 9, 12}, gotCondChecks); diff != "" {
			t.Errorf("cond checks diff (-want +got):\n%s", diff)
		}
		if got, want := gotFinal, 12; got != want {
			t.Errorf("got %d; want %d", got, want)
		}
		if got, want := *mon.Close(), n*incr; got != want {
			t.Errorf("Close() got %d; want %d", got, want)
		}
	})
}
