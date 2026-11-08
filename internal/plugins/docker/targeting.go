package docker

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/gabriwl165/daffy/internal/plugins"
)

var safeContext = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var safeSelector = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

type requestRunnerKey struct{}
type selectorKey struct{}
type scopedRunner struct {
	base    Runner
	context string
}

func (r scopedRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	if r.context != "" {
		args = append([]string{"--context", r.context}, args...)
	}
	return r.base.Run(ctx, args...)
}
func runnerFor(ctx context.Context, fallback Runner) Runner {
	if r, ok := ctx.Value(requestRunnerKey{}).(Runner); ok {
		return r
	}
	return fallback
}
func targetSelectors(ctx context.Context) []string {
	v, _ := ctx.Value(selectorKey{}).([]string)
	return v
}

func validateTargetOptions(opts plugins.Options) error {
	if opts.Context != "" && !safeContext.MatchString(opts.Context) {
		return plugins.ErrInvalid
	}
	for _, v := range opts.Containers {
		if !safeSelector.MatchString(v) {
			return plugins.ErrInvalid
		}
	}
	return nil
}

type listedContainer struct {
	ID    string `json:"ID"`
	Names string `json:"Names"`
}

// selectContainers resolves exact names and unambiguous ID prefixes against an all-state list.
func (p *Plugin) selectContainers(ctx context.Context, selectors []string) ([]string, error) {
	b, err := p.call(ctx, "docker ps", "ps", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	rows, err := decodeLines[listedContainer](b)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxContainers {
		return nil, errors.New("Docker container count exceeded limit")
	}
	if len(selectors) == 0 {
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			if !validID(r.ID) {
				return nil, ErrInvalidResponse
			}
			ids = append(ids, r.ID)
		}
		return ids, nil
	}
	selected := make([]string, 0, len(selectors))
	seen := map[string]bool{}
	for _, s := range selectors {
		matches := []string{}
		for _, r := range rows {
			if !validID(r.ID) {
				return nil, ErrInvalidResponse
			}
			name := strings.TrimPrefix(r.Names, "/")
			if name == s || len(s) >= 12 && strings.HasPrefix(r.ID, s) {
				matches = append(matches, r.ID)
			}
		}
		if len(matches) != 1 {
			return nil, plugins.ErrInvalid
		}
		if !seen[matches[0]] {
			selected = append(selected, matches[0])
			seen[matches[0]] = true
		}
	}
	return selected, nil
}
