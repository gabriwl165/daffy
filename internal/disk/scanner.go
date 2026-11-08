package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type scanner struct{ platform Platform }

func (s *scanner) Scan(ctx context.Context, c Config) (Report, error) {
	if c.TopN < 0 || c.TopN > 10000 {
		return Report{}, fmt.Errorf("TopN must be between 0 and 10000")
	}
	if c.TimeBudget < 0 {
		return Report{}, fmt.Errorf("TimeBudget cannot be negative")
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 128
	}
	if c.TimeBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.TimeBudget)
		defer cancel()
	}
	root := c.Path
	if root == "" {
		root = "/"
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	report := Report{Root: root, Coverage: Coverage{Complete: true}}
	if _, err := os.Lstat(root); err != nil {
		return report, fmt.Errorf("scan root %q: %w", root, err)
	}
	mounts, err := s.platform.Mounts()
	if err != nil {
		if c.Path == "" {
			return report, err
		}
	}
	report.Coverage.Mounts = len(mounts)
	if c.Path != "" {
		mounts = []Mount{{Path: root, Eligible: true}}
	} else {
		// Rooted traversal sees mount boundaries as ordinary directories. Excluding every
		// nested mount avoids aliases and prevents crossing filesystem boundaries.
		for _, m := range mounts {
			if m.Path != "/" {
				c.Excludes = append(c.Excludes, m.Path)
			}
		}
	}
	excludes := make([]string, 0, len(c.Excludes))
	for _, p := range c.Excludes {
		a, e := filepath.Abs(p)
		if e == nil {
			excludes = append(excludes, filepath.Clean(a))
		}
	}
	for _, m := range mounts {
		if !m.Eligible {
			report.Coverage.SkippedMounts++
			report.Issues = appendIssue(report.Issues, Issue{Path: m.Path, Operation: "mount-skip", Error: m.Reason})
			continue
		}
		if m.Path != "" {
			cap, e := s.platform.Capacity(m.Path)
			if e == nil {
				cap.Path = m.Path
				cap.Filesystem = m.Filesystem
				report.Mounts = append(report.Mounts, cap)
			}
		}
	}
	type node struct {
		path                string
		apparent, allocated int64
		dirs                []node
	}
	seen := map[[2]uint64]string{}
	issues := 0
	maxIssues := 100
	var walk func(string) (node, error)
	walk = func(path string) (node, error) {
		n := node{path: path}
		if e := ctx.Err(); e != nil {
			return n, e
		}
		for _, x := range excludes {
			if path == x || strings.HasPrefix(path, x+string(filepath.Separator)) {
				return n, nil
			}
		}
		fi, e := os.Lstat(path)
		if e != nil {
			issues++
			report.Coverage.SkippedEntries++
			report.Coverage.OmittedErrors++
			if issues <= maxIssues {
				report.Issues = appendIssue(report.Issues, Issue{path, "lstat", e.Error()})
			}
			return n, nil
		}
		sz, blocks, dev, ino, e := metadata(path)
		if e == nil {
			if fi.Mode().IsRegular() {
				key := [2]uint64{dev, ino}
				if _, ok := seen[key]; ok {
					sz = 0
					blocks = 0
				} else {
					seen[key] = path
				}
			}
			n.apparent, n.allocated = sz, blocks
		}
		if fi.IsDir() {
			report.Coverage.Directories++
			f, e := os.Open(path)
			if e != nil {
				issues++
				report.Coverage.SkippedEntries++
				report.Coverage.OmittedErrors++
				return n, nil
			}
			for {
				entries, e := f.ReadDir(c.BatchSize)
				for _, ent := range entries {
					child, e := walk(filepath.Join(path, ent.Name()))
					if e != nil {
						f.Close()
						return n, e
					}
					n.apparent += child.apparent
					n.allocated += child.allocated
				}
				if e == io.EOF {
					break
				}
				if e != nil {
					issues++
					report.Coverage.SkippedEntries++
					break
				}
				if ctx.Err() != nil {
					f.Close()
					return n, ctx.Err()
				}
			}
			f.Close()
		} else if fi.Mode().IsRegular() {
			report.Coverage.Files++
		}
		return n, nil
	}
	for _, m := range mounts {
		if e := ctx.Err(); e != nil {
			report.Coverage.Complete = false
			return report, e
		}
		p := m.Path
		if c.Path != "" {
			p = root
		}
		if p == "" {
			continue
		}
		n, e := walk(p)
		if e != nil {
			report.Coverage.Complete = false
			return report, e
		}
		report.Totals.ApparentBytes += n.apparent
		report.Totals.AllocatedBytes += n.allocated
	}
	if ctx.Err() != nil {
		report.Coverage.Complete = false
		return report, ctx.Err()
	}
	return report, nil
}

func appendIssue(issues []Issue, i Issue) []Issue {
	if len(issues) < 100 {
		return append(issues, i)
	}
	return issues
}
