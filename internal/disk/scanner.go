package disk

import (
	"container/heap"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	DefaultBatchSize                  = 128
	MaxBatchSize                      = 4096
	MaxReportIssues                   = 100
	MaxReportCandidates               = 1000
	OperationStat           Operation = "stat"
	OperationMetadata       Operation = "metadata"
	OperationExclude        Operation = "exclude"
	OperationCloseDirectory Operation = "close-directory"
	OperationCandidateLimit Operation = "candidate-limit"
	OperationOpenMount      Operation = "open-mount"
	OperationPlan           Operation = "mount-plan"
)

type scanner struct {
	platform  Platform
	openRoot  func(string) (Directory, error)
	candidate func(string, Totals) (Candidate, bool)
}

func (s *scanner) open(path string) (Directory, error) {
	if s.openRoot != nil {
		return s.openRoot(path)
	}
	return openScanRoot(path)
}

type rankedEntry struct {
	path   string
	totals Totals
}
type rankHeap []rankedEntry

func (h rankHeap) Len() int { return len(h) }
func (h rankHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.totals.AllocatedBytes != b.totals.AllocatedBytes {
		return a.totals.AllocatedBytes < b.totals.AllocatedBytes
	}
	return a.path > b.path
}
func (h rankHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *rankHeap) Push(v any)   { *h = append(*h, v.(rankedEntry)) }
func (h *rankHeap) Pop() any     { x := *h; n := len(x); v := x[n-1]; *h = x[:n-1]; return v }
func offer(h *rankHeap, v rankedEntry, n int) {
	if n == 0 {
		return
	}
	if h.Len() < n {
		heap.Push(h, v)
		return
	}
	if rankedBefore(v, (*h)[0]) {
		heap.Pop(h)
		heap.Push(h, v)
	}
}

func (s *scanner) Scan(parent context.Context, c Config) (report Report, retErr error) {
	if c.TopN < 0 || c.TopN > MaxTopN {
		return Report{}, fmt.Errorf("TopN must be between 0 and %d", MaxTopN)
	}
	if c.TimeBudget < 0 {
		return Report{}, fmt.Errorf("TimeBudget cannot be negative")
	}
	if c.BatchSize < 0 || c.BatchSize > MaxBatchSize {
		return Report{}, fmt.Errorf("BatchSize must be between 0 and %d", MaxBatchSize)
	}
	if c.BatchSize == 0 {
		c.BatchSize = DefaultBatchSize
	}
	ctx := parent
	cancel := func() {}
	if c.TimeBudget > 0 {
		ctx, cancel = context.WithTimeout(parent, c.TimeBudget)
	}
	defer cancel()
	selected := c.Path
	if selected == "" {
		selected = "/"
	}
	selected, e := filepath.Abs(selected)
	if e != nil {
		return Report{}, e
	}
	selected = filepath.Clean(selected)
	report = Report{Root: selected, Coverage: Coverage{Complete: true}}
	finalized := false
	files, dirs := rankHeap{}, rankHeap{}
	heap.Init(&files)
	heap.Init(&dirs)
	defer func() {
		if !finalized {
			finishRanking(&report, files, dirs)
		}
	}()
	if e := ctx.Err(); e != nil {
		report.Coverage.Complete = false
		return report, e
	}
	mounts, e := s.platform.Mounts()
	if e != nil {
		report.Coverage.Complete = false
		report.Issues = append(report.Issues, Issue{Path: selected, Operation: OperationMountDiscovery, Error: e.Error()})
		return report, fmt.Errorf("discover mounts: %w", e)
	}
	plan, e := BuildMountPlan(mounts, c.Path, c.Excludes)
	if e != nil {
		report.Coverage.Complete = false
		report.Coverage.Mounts = len(mounts)
		report.Coverage.SkippedMounts = 1
		report.Issues = append(report.Issues, Issue{Path: selected, Operation: OperationPlan, Error: e.Error()})
		return report, e
	}
	addIssue := func(path string, op Operation, message string, isError bool) {
		if len(report.Issues) < MaxReportIssues {
			report.Issues = append(report.Issues, Issue{Path: path, Operation: op, Error: message})
		} else if isError {
			report.Coverage.OmittedErrors++
		}
		if isError {
			switch op {
			case OperationStat, OperationMetadata, OperationReadDir, OperationOpenDirectory:
				report.Coverage.SkippedEntries++
			}
			report.Coverage.Complete = false
		}
	}
	selectedMounts := append([]Mount(nil), plan.Roots...)
	for _, m := range plan.Skipped {
		if selected == "/" || within(selected, m.Path) || within(m.Path, selected) {
			selectedMounts = append(selectedMounts, m)
		}
	}
	report.Coverage.Mounts = len(selectedMounts)
	for _, m := range plan.Skipped {
		if selected != "/" && !within(selected, m.Path) && !within(m.Path, selected) {
			continue
		}
		report.Coverage.SkippedMounts++
		addIssue(m.Path, OperationMountSkip, string(m.SkipReason), false)
	}
	for _, m := range plan.Roots {
		if err := ctx.Err(); err != nil {
			report.Coverage.Complete = false
			return report, err
		}
		scanPath := rootForMount(m, selected, c.Path != "")
		if plan.Exclusions.Contains(scanPath) {
			report.Coverage.SkippedMounts++
			addIssue(scanPath, OperationExclude, "mount root excluded by configuration", false)
			continue
		}
		cap, ce := s.platform.Capacity(m.Path)
		if ce != nil {
			addIssue(m.Path, OperationCapacity, ce.Error(), true)
		} else {
			cap.Path = m.Path
			cap.Filesystem = m.Filesystem
			report.Mounts = append(report.Mounts, cap)
		}
	}
	seen := make(map[FileIdentity]Path)
	candidateLimitNoted := false
	var walk func(Directory, string, bool) (Totals, error)
	walk = func(d Directory, path string, isRoot bool) (Totals, error) {
		var total Totals
		if e := ctx.Err(); e != nil {
			return total, e
		}
		if !isRoot && plan.Exclusions.Contains(path) {
			report.Coverage.SkippedEntries++
			return total, nil
		}
		if !isRoot {
			if _, boundary := plan.Boundaries[Path(path)]; boundary {
				return total, nil
			}
		}
		fi, e := d.Stat()
		if e != nil {
			addIssue(path, OperationStat, e.Error(), true)
			return total, nil
		}
		meta, e := entryMetadata(fi)
		if e != nil {
			addIssue(path, OperationMetadata, e.Error(), true)
			return total, nil
		}
		if e = ctx.Err(); e != nil {
			return total, e
		}
		total = Totals{ApparentBytes: meta.ApparentBytes, AllocatedBytes: meta.AllocatedBytes}
		if meta.Mode.IsDir() {
			report.Coverage.Directories++
			for {
				entries, re := d.ReadDir(c.BatchSize)
				for _, ent := range entries {
					if ctx.Err() != nil {
						return total, ctx.Err()
					}
					name := ent.Name()
					childPath := filepath.Join(path, name)
					if plan.Exclusions.Contains(childPath) {
						report.Coverage.SkippedEntries++
						continue
					}
					if _, boundary := plan.Boundaries[Path(childPath)]; boundary {
						continue
					}
					cm, me := d.ChildMetadata(name)
					if me != nil {
						addIssue(childPath, OperationMetadata, me.Error(), true)
						continue
					}
					if ce := ctx.Err(); ce != nil {
						return total, ce
					}
					child := Totals{ApparentBytes: cm.ApparentBytes, AllocatedBytes: cm.AllocatedBytes}
					if cm.Mode.IsDir() {
						cd, oe := d.OpenChild(name)
						if oe != nil {
							addIssue(childPath, OperationOpenDirectory, oe.Error(), true)
							continue
						}
						child, e = walk(cd, childPath, false)
						closeErr := cd.Close()
						if closeErr != nil {
							addIssue(childPath, OperationCloseDirectory, closeErr.Error(), true)
						}
						total.ApparentBytes += child.ApparentBytes
						total.AllocatedBytes += child.AllocatedBytes
						if e != nil {
							return total, e
						}
						continue
					}
					if cm.Mode.IsRegular() {
						report.Coverage.Files++
						if cm.Links > 1 {
							if _, exists := seen[cm.Identity]; exists {
								continue
							}
							seen[cm.Identity] = Path(childPath)
						}
						offer(&files, rankedEntry{path: childPath, totals: child}, c.TopN)
					}
					if cm.Mode.IsRegular() || cm.Mode&os.ModeSymlink != 0 {
						total.ApparentBytes += child.ApparentBytes
						total.AllocatedBytes += child.AllocatedBytes
					}
				}
				if re == io.EOF {
					break
				}
				if re != nil {
					addIssue(path, OperationReadDir, re.Error(), true)
					break
				}
				if ce := ctx.Err(); ce != nil {
					return total, ce
				}
			}
			if s.candidate != nil {
				if candidate, ok := s.candidate(path, total); ok {
					if len(report.Candidates) < MaxReportCandidates {
						report.Candidates = append(report.Candidates, candidate)
					} else if !candidateLimitNoted {
						addIssue(path, OperationCandidateLimit, "candidate report limit reached", false)
						candidateLimitNoted = true
					}
				}
			}
			offer(&dirs, rankedEntry{path: path, totals: total}, c.TopN)
		}
		return total, nil
	}
	for _, m := range plan.Roots {
		if ctx.Err() != nil {
			report.Coverage.Complete = false
			return report, ctx.Err()
		}
		path := rootForMount(m, selected, c.Path != "")
		if plan.Exclusions.Contains(path) {
			continue
		}
		d, oe := s.open(path)
		if oe != nil {
			report.Coverage.SkippedMounts++
			addIssue(path, OperationOpenMount, oe.Error(), true)
			if c.Path != "" {
				return report, fmt.Errorf("open scan root %q: %w", path, oe)
			}
			continue
		}
		report.Coverage.ScannedMounts++
		totals, walkErr := walk(d, path, true)
		closeErr := d.Close()
		report.Totals.ApparentBytes += totals.ApparentBytes
		report.Totals.AllocatedBytes += totals.AllocatedBytes
		if closeErr != nil {
			addIssue(path, OperationCloseDirectory, closeErr.Error(), true)
		}
		if walkErr != nil {
			report.Coverage.Complete = false
			return report, walkErr
		}
	}
	if ctx.Err() != nil {
		report.Coverage.Complete = false
		return report, ctx.Err()
	}
	finishRanking(&report, files, dirs)
	finalized = true
	return report, nil
}

func rootForMount(m Mount, selected string, explicit bool) string {
	if explicit && within(m.Path, selected) {
		return selected
	}
	return m.Path
}

func finishRanking(r *Report, files, dirs rankHeap) {
	fs := append(rankHeap(nil), files...)
	ds := append(rankHeap(nil), dirs...)
	sort.Slice(fs, func(i, j int) bool { return rankedBefore(fs[i], fs[j]) })
	sort.Slice(ds, func(i, j int) bool { return rankedBefore(ds[i], ds[j]) })
	for _, v := range fs {
		r.TopFiles = append(r.TopFiles, FileEntry{Path: v.path, ApparentBytes: v.totals.ApparentBytes, AllocatedBytes: v.totals.AllocatedBytes})
	}
	for _, v := range ds {
		r.TopDirectories = append(r.TopDirectories, DirectoryEntry{Path: v.path, ApparentBytes: v.totals.ApparentBytes, AllocatedBytes: v.totals.AllocatedBytes})
	}
}
func rankedBefore(a, b rankedEntry) bool {
	if a.totals.AllocatedBytes != b.totals.AllocatedBytes {
		return a.totals.AllocatedBytes > b.totals.AllocatedBytes
	}
	return a.path < b.path
}
