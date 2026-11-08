//go:build !linux

package disk

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsForAllAndExplicitPaths(t *testing.T) {
	for _, path := range []string{"", "/tmp"} {
		r, e := Scan(context.Background(), Config{Path: path})
		if !errors.Is(e, ErrUnsupportedPlatform) {
			t.Fatalf("path %q: expected unsupported error, report=%+v err=%v", path, r, e)
		}
	}
}
