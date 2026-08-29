package processor

import (
	"context"
	"time"
)

// retry runs fn once per element of delays, waiting delays[i] before attempt
// i (delays[0] is usually 0). It returns nil on the first success, the last
// error otherwise, and ctx.Err() if the context is cancelled while waiting.
// An empty delays slice performs no attempts and returns nil.
func retry(ctx context.Context, delays []time.Duration, fn func() error) error {
	var last error
	for _, d := range delays {
		if d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		if last = fn(); last == nil {
			return nil
		}
	}
	return last
}
