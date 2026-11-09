package docker

import (
	"context"
	"errors"
)

// normalizeRunnerError retains only known categories, never a runner's raw text.
// Wrapped errors can contain command output even when they wrap a safe sentinel.
func normalizeRunnerError(ctx context.Context, command string, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &commandTimeoutError{command: command, err: context.DeadlineExceeded}
	}
	for _, category := range []error{context.Canceled, ErrCommandFailed, ErrOutputLimit, ErrUnavailable, ErrPermission, ErrConnection, ErrUnsupported} {
		if errors.Is(err, category) {
			return category
		}
	}
	return ErrCommandFailed
}
