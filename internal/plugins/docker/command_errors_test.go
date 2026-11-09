package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRunnerErrorNormalizationNeverExportsRawText(t *testing.T) {
	for _, raw := range []error{
		errors.New("private log token"),
		fmt.Errorf("private log token: %w", ErrPermission),
		fmt.Errorf("private log token: %w", context.DeadlineExceeded),
	} {
		p := New(runnerFunc(func(context.Context, ...string) ([]byte, error) { return nil, raw }))
		for _, logs := range []bool{false, true} {
			var err error
			if logs {
				_, err = p.callLogs(context.Background(), "logs", "container")
			} else {
				_, err = p.call(context.Background(), "docker inspect", "container", "inspect")
			}
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("logs=%t: raw runner text escaped: %v", logs, err)
			}
			if errors.Is(raw, ErrPermission) && !errors.Is(err, ErrPermission) {
				t.Fatalf("lost permission category: %v", err)
			}
			if errors.Is(raw, context.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost timeout category: %v", err)
			}
		}
	}
}
