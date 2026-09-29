package telegram_test

import (
	"context"
	"errors"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestPoller(t *testing.T) {
	t.Run("handles every update in order and polls again after the last one handled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source := &scriptedUpdates{batches: [][]telegram.Update{
			{{UpdateID: 10}, {UpdateID: 11}},
			{{UpdateID: 12}},
		}, whenDone: cancel}
		var handled []int64
		poller := &telegram.Poller{
			Updates: source,
			Handler: telegram.HandlerFunc(func(_ context.Context, update telegram.Update) error {
				handled = append(handled, update.UpdateID)
				return nil
			}),
			Timeout: 30 * time.Second,
		}

		if err := poller.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("run: got %v, want %v", err, context.Canceled)
		}

		if want := []int64{10, 11, 12}; !reflect.DeepEqual(handled, want) {
			t.Errorf("handled updates: got %v, want %v", handled, want)
		}
		if want := []int64{0, 12, 13}; !reflect.DeepEqual(source.offsets, want) {
			t.Errorf("polled offsets: got %v, want %v", source.offsets, want)
		}
		for _, timeout := range source.timeouts {
			if timeout != 30*time.Second {
				t.Errorf("poll timeout: got %v, want %v", timeout, 30*time.Second)
			}
		}
	})

	t.Run("reports a failed update and keeps handling the rest", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source := &scriptedUpdates{batches: [][]telegram.Update{{{UpdateID: 1}, {UpdateID: 2}}}, whenDone: cancel}
		handleErr := errors.New("reply failed")
		var handled []int64
		var reported []error
		poller := &telegram.Poller{
			Updates: source,
			Handler: telegram.HandlerFunc(func(_ context.Context, update telegram.Update) error {
				handled = append(handled, update.UpdateID)
				if update.UpdateID == 1 {
					return handleErr
				}
				return nil
			}),
			OnError: func(err error) { reported = append(reported, err) },
		}

		poller.Run(ctx)

		if want := []int64{1, 2}; !reflect.DeepEqual(handled, want) {
			t.Errorf("handled updates: got %v, want %v", handled, want)
		}
		if len(reported) != 1 || !errors.Is(reported[0], handleErr) {
			t.Errorf("reported errors: got %v, want [%v]", reported, handleErr)
		}
	})

	t.Run("reports a failed poll and retries from the same offset", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pollErr := errors.New("connection reset")
		source := &scriptedUpdates{
			batches:  [][]telegram.Update{{{UpdateID: 5}}, nil, {{UpdateID: 6}}},
			failures: map[int]error{1: pollErr},
			whenDone: cancel,
		}
		var reported []error
		poller := &telegram.Poller{
			Updates:    source,
			Handler:    telegram.HandlerFunc(func(context.Context, telegram.Update) error { return nil }),
			RetryDelay: time.Millisecond,
			OnError:    func(err error) { reported = append(reported, err) },
		}

		poller.Run(ctx)

		if want := []int64{0, 6, 6, 7}; !reflect.DeepEqual(source.offsets, want) {
			t.Errorf("polled offsets: got %v, want %v", source.offsets, want)
		}
		if len(reported) != 1 || !errors.Is(reported[0], pollErr) {
			t.Errorf("reported errors: got %v, want [%v]", reported, pollErr)
		}
	})

	t.Run("reports recovery with the number of failed polls once a poll succeeds again", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pollErr := errors.New("connection reset")
		source := &scriptedUpdates{
			batches:  [][]telegram.Update{nil, nil, nil, nil, nil},
			failures: map[int]error{1: pollErr, 2: pollErr, 4: pollErr},
			whenDone: cancel,
		}
		var recoveries []int
		poller := &telegram.Poller{
			Updates:    source,
			Handler:    telegram.HandlerFunc(func(context.Context, telegram.Update) error { return nil }),
			RetryDelay: time.Millisecond,
			OnError:    func(error) {},
			OnRecover:  func(failedPolls int) { recoveries = append(recoveries, failedPolls) },
		}

		poller.Run(ctx)

		if want := []int{2}; !reflect.DeepEqual(recoveries, want) {
			t.Errorf("recoveries: got %v, want %v", recoveries, want)
		}
	})

	t.Run("logs failures and recovery when no hooks are set", func(t *testing.T) {
		var logged strings.Builder
		log.SetOutput(&logged)
		flags := log.Flags()
		log.SetFlags(0)
		t.Cleanup(func() {
			log.SetOutput(os.Stderr)
			log.SetFlags(flags)
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		source := &scriptedUpdates{
			batches:  [][]telegram.Update{nil, {{UpdateID: 3}}},
			failures: map[int]error{0: errors.New("connection reset")},
			whenDone: cancel,
		}
		poller := &telegram.Poller{
			Updates:    source,
			Handler:    telegram.HandlerFunc(func(context.Context, telegram.Update) error { return errors.New("reply failed") }),
			RetryDelay: time.Millisecond,
		}

		poller.Run(ctx)

		want := "poll telegram updates: connection reset\n" +
			"telegram polling recovered after 1 failed poll(s)\n" +
			"handle telegram update 3: reply failed\n"
		if logged.String() != want {
			t.Errorf("log: got %q, want %q", logged.String(), want)
		}
	})

	t.Run("stops with the conflict when Telegram refuses to long poll", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		conflict := &telegram.APIError{Method: "getUpdates", Code: 409, Description: "Conflict: can't use getUpdates method while webhook is active; use deleteWebhook to delete the webhook first"}
		source := &scriptedUpdates{
			batches:  [][]telegram.Update{nil, nil},
			failures: map[int]error{0: conflict, 1: conflict},
			whenDone: cancel,
		}
		poller := &telegram.Poller{
			Updates:    source,
			Handler:    telegram.HandlerFunc(func(context.Context, telegram.Update) error { return nil }),
			RetryDelay: time.Millisecond,
			OnError:    func(error) {},
		}

		err := poller.Run(ctx)

		if !errors.Is(err, telegram.ErrPollingConflict) || !errors.Is(err, conflict) {
			t.Fatalf("run: got %v, want it to wrap %v and %v", err, telegram.ErrPollingConflict, conflict)
		}
		if len(source.offsets) != 1 {
			t.Errorf("polls: got %d, want 1", len(source.offsets))
		}
	})
}

// scriptedUpdates returns batches in order, failing the poll whose index is in failures, and calls
// whenDone once every batch has been returned.
type scriptedUpdates struct {
	batches  [][]telegram.Update
	failures map[int]error
	whenDone func()
	offsets  []int64
	timeouts []time.Duration
}

func (s *scriptedUpdates) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]telegram.Update, error) {
	call := len(s.offsets)
	s.offsets = append(s.offsets, offset)
	s.timeouts = append(s.timeouts, timeout)
	if call >= len(s.batches) {
		s.whenDone()
		return nil, ctx.Err()
	}
	if err := s.failures[call]; err != nil {
		return nil, err
	}
	return s.batches[call], nil
}
