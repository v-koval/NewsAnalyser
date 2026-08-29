package processor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetrySucceedsAfterFailures(t *testing.T) {
	calls := 0
	err := retry(context.Background(), []time.Duration{0, time.Millisecond, time.Millisecond}, func() error {
		calls++
		if calls < 3 {
			return errors.New("boom")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want nil and 3", err, calls)
	}
}

func TestRetryReturnsLastError(t *testing.T) {
	calls := 0
	err := retry(context.Background(), []time.Duration{0, time.Millisecond}, func() error {
		calls++
		return errors.New("always")
	})
	if err == nil || err.Error() != "always" || calls != 2 {
		t.Fatalf("err=%v calls=%d, want 'always' and 2", err, calls)
	}
}

func TestRetryStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := retry(ctx, []time.Duration{0, time.Hour}, func() error {
		calls++
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want context.Canceled and 1", err, calls)
	}
}
