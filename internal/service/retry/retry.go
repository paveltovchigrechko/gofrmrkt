package retry

import (
	"context"
	"time"
)

const maxRetries = 3

type RetryFunc func() error

type ShouldRetry func(error) bool

func Do(ctx context.Context, shouldRetry ShouldRetry, fn RetryFunc) error {
	var lastErr error
	intervals := []time.Duration{
		1 * time.Second,
		3 * time.Second,
		5 * time.Second,
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check the operation context.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Run the function.
		err := fn()
		if err == nil {
			return nil
		}

		// Check the error.
		if !shouldRetry(err) {
			return err
		}
		// Save the error.
		lastErr = err

		if attempt == maxRetries {
			break
		}

		// Retry logic.
		// We shouldn't sleep if the operation has already been cancelled.
		// Select what finishes first: interval or operation.
		select {
		case <-time.After(intervals[attempt]):
			continue

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}
