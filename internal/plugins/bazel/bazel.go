// Package bazel implements read-only disk analysis for user-selected Bazel output paths.
package bazel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gabriwl165/daffy/internal/disk"
	"github.com/gabriwl165/daffy/internal/plugins"
)

// Plugin reports observed filesystem usage under a path explicitly supplied by the user.
// It does not invoke Bazel or infer that observed files are cache data.
type Plugin struct{ scanner disk.Scanner }

const (
	maxTopN          = disk.MaxTopN
	issueInvalidPath = "invalid-path"
	issueCancelled   = "cancelled"
	issueScanError   = "scan-error"
)

func New(scanner disk.Scanner) *Plugin {
	if scanner == nil {
		scanner = disk.NewScanner()
	}
	return &Plugin{scanner: scanner}
}

func (p *Plugin) Describe() plugins.Descriptor {
	return plugins.Descriptor{ID: plugins.PluginBazel, Operations: []plugins.Operation{{
		ID:      plugins.OperationDisk,
		Summary: "Report observed filesystem usage under a user-supplied Bazel output or cache path.",
		Default: true, Access: plugins.AccessFilesystem,
		Options: plugins.SupportedOptions{Path: true, Excludes: true, TopN: true, RequirePath: true},
	}}}
}

func (p *Plugin) Analyze(ctx context.Context, req plugins.Request) (plugins.Outcome, error) {
	out := plugins.Outcome{Operation: plugins.OperationDisk, Status: plugins.StatusUnavailable}
	if req.Operation != "" && req.Operation != plugins.OperationDisk {
		return out, fmt.Errorf("%w: %q", plugins.ErrUnknownOperation, req.Operation)
	}
	path := req.Options.Path
	if path == "" {
		return out, fmt.Errorf("%w: explicit path required", plugins.ErrInvalid)
	}
	if req.Options.TopN < 0 || req.Options.TopN > maxTopN {
		return out, fmt.Errorf("%w: TopN must be between 0 and %d", plugins.ErrInvalid, maxTopN)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		out.Issues = []plugins.Issue{{Code: issueInvalidPath, Summary: err.Error()}}
		return out, fmt.Errorf("%w: validate scan path: %v", plugins.ErrInvalid, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		err = fmt.Errorf("scan path must not be a symlink")
		out.Issues = []plugins.Issue{{Code: issueInvalidPath, Summary: err.Error()}}
		return out, fmt.Errorf("%w: %v", plugins.ErrInvalid, err)
	}
	if !fi.IsDir() {
		err = fmt.Errorf("scan path must be a directory")
		out.Issues = []plugins.Issue{{Code: issueInvalidPath, Summary: err.Error()}}
		return out, fmt.Errorf("%w: %v", plugins.ErrInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		out.Status = plugins.StatusPartial
		out.Issues = []plugins.Issue{{Code: issueCancelled, Summary: err.Error()}}
		return out, err
	}
	report, scanErr := p.scanner.Scan(ctx, disk.Config{Path: filepath.Clean(path), Excludes: append([]string(nil), req.Options.Excludes...), TopN: req.Options.TopN})
	out.Details.Bazel = &plugins.BazelDetails{Disk: &report}
	if len(report.Issues) > 0 || report.Coverage.SkippedEntries > 0 || report.Coverage.OmittedErrors > 0 || report.Coverage.SkippedMounts > 0 || !report.Coverage.Complete || scanErr != nil {
		out.Status = plugins.StatusPartial
	} else {
		out.Status = plugins.StatusComplete
	}
	if scanErr != nil {
		code := issueScanError
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			code = issueCancelled
		}
		out.Issues = append(out.Issues, plugins.Issue{Code: code, Summary: scanErr.Error()})
		return out, scanErr
	}
	return out, nil
}
