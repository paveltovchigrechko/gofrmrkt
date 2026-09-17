package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var errTransient = errors.New("transient error")
var errFatal = errors.New("fatal error")

func retryableFilter(err error) bool {
	return errors.Is(err, errTransient)
}

func TestDo(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			return nil
		}

		err := Do(context.Background(), retryableFilter, fn)

		assert.NoError(t, err)
		assert.Equal(t, 1, attempts)
	})

	t.Run("stops immediately on non-retryable error", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			return errFatal
		}

		err := Do(context.Background(), retryableFilter, fn)

		assert.ErrorIs(t, err, errFatal)
		assert.Equal(t, 1, attempts, "Should not retry if shouldRetry returns false")
	})

	t.Run("returns context error if context cancelled before execution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		attempts := 0
		fn := func() error {
			attempts++
			return nil
		}

		err := Do(ctx, retryableFilter, fn)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 0, attempts, "Function should not run if context is already canceled")
	})

	t.Run("aborts retry wait when context is cancelled during delay", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		attempts := 0
		fn := func() error {
			attempts++
			return errTransient
		}

		start := time.Now()
		err := Do(ctx, retryableFilter, fn)
		elapsed := time.Since(start)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, attempts)
		assert.Less(t, elapsed, 1*time.Second, "Should cancel wait early when context expires")
	})
}
