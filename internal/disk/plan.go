package disk

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const MaxTopN = 1000

type MountPlan struct {
	Roots      []Mount
	Boundaries map[Path]Mount
	Skipped    []Mount
	Exclusions PathTrie
	ScanRoot   Path
}
type Device string

// PathTrie stores normalized absolute path components. Construction is O(total
// input path components); Contains/HasAncestor is O(path components), independent
// of the number of exclusions or mount boundaries.
type PathTrie struct {
	children map[string]*PathTrie
	terminal bool
	subtree  bool
}

func (t *PathTrie) Add(path string) error {
	p, e := normalizePath(path)
	if e != nil {
		return e
	}
	if t.children == nil {
		t.children = make(map[string]*PathTrie)
	}
	cur := t
	for _, part := range components(p) {
		if cur.children[part] == nil {
			cur.children[part] = &PathTrie{children: make(map[string]*PathTrie)}
		}
		cur = cur.children[part]
	}
	cur.terminal = true
	cur = t
	cur.subtree = true
	for _, part := range components(p) {
		cur = cur.children[part]
		cur.subtree = true
	}
	return nil
}
func (t *PathTrie) Contains(path string) bool {
	p, e := normalizePath(path)
	if e != nil {
		return false
	}
	cur := t
	for _, part := range components(p) {
		if cur == nil {
			return false
		}
		if cur.terminal {
			return true
		}
		cur = cur.children[part]
	}
	return cur != nil && cur.terminal
}
func (t *PathTrie) HasAncestor(path string) bool { return t.Contains(path) }

// Overlaps reports an ancestor or descendant terminal path in O(path components).
func (t *PathTrie) Overlaps(path string) bool {
	p, e := normalizePath(path)
	if e != nil {
		return false
	}
	cur := t
	for _, part := range components(p) {
		if cur == nil {
			return false
		}
		if cur.terminal {
			return true
		}
		cur = cur.children[part]
	}
	return cur != nil && cur.subtree
}
func components(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}
func normalizePath(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute: %q", p)
	}
	return filepath.Clean(p), nil
}

// BuildMountPlan is pure and performs all mount filtering, boundary collection,
// alias suppression and path selection once before traversal.
func BuildMountPlan(mounts []Mount, selected string, exclusions []string) (MountPlan, error) {
	p := MountPlan{Boundaries: make(map[Path]Mount)}
	if selected == "" {
		selected = "/"
	}
	root, e := filepath.Abs(selected)
	if e != nil {
		return p, e
	}
	root, e = normalizePath(root)
	if e != nil {
		return p, e
	}
	p.ScanRoot = Path(root)
	all := append([]Mount(nil), mounts...)
	for i := range all {
		all[i].Path, e = normalizePath(all[i].Path)
		if e != nil {
			return p, fmt.Errorf("mountpoint: %w", e)
		}
		if all[i].Root == "" {
			all[i].Root = "/"
		} else {
			if filepath.IsAbs(all[i].Root) {
				all[i].Root, e = normalizePath(all[i].Root)
				if e != nil {
					return p, fmt.Errorf("mount root: %w", e)
				}
			} else if eligible(all[i]) {
				return p, fmt.Errorf("eligible mount has invalid filesystem root %q", all[i].Root)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if len(all[i].Path) != len(all[j].Path) {
			return len(all[i].Path) < len(all[j].Path)
		}
		return all[i].Path < all[j].Path
	})
	// Mountinfo can contain stacked mounts at the same path. Without retaining
	// mount IDs we cannot safely choose the visible layer, so represent each
	// duplicate group as one ineligible boundary.
	unique := make([]Mount, 0, len(all))
	for i := 0; i < len(all); {
		j := i + 1
		for j < len(all) && all[j].Path == all[i].Path {
			j++
		}
		if j-i > 1 {
			ambiguous := all[i]
			ambiguous.Eligible = false
			ambiguous.SkipReason = SkipAmbiguousMount
			unique = append(unique, ambiguous)
			p.Skipped = append(p.Skipped, ambiguous)
		} else {
			unique = append(unique, all[i])
		}
		i = j
	}
	all = unique
	for _, x := range exclusions {
		if e := p.Exclusions.Add(x); e != nil {
			return p, e
		}
	}
	for _, m := range all {
		p.Boundaries[Path(m.Path)] = m
	}
	var containing *Mount
	if selected != "/" {
		for i := range all {
			if within(all[i].Path, root) && (containing == nil || len(all[i].Path) > len(containing.Path)) {
				containing = &all[i]
			}
		}
		if containing != nil && containing.SkipReason == SkipAmbiguousMount {
			return p, fmt.Errorf("selected path %q is beneath ambiguous mountpoint %q", root, containing.Path)
		}
		if containing == nil || !eligible(*containing) {
			return p, fmt.Errorf("selected path %q is on an unsupported filesystem", root)
		}
	}
	candidates := make([]Mount, 0, len(all))
	for _, m := range all {
		if m.SkipReason == SkipAmbiguousMount {
			continue
		}
		if !eligible(m) {
			m.Eligible = false
			if removablePath(m.Path) {
				m.SkipReason = SkipRemovable
			} else if m.Filesystem == FilesystemOverlay && m.Path != "/" {
				m.SkipReason = SkipNestedOverlay
			} else if m.SkipReason == "" {
				m.SkipReason = SkipUnsupportedFilesystem
			}
			p.Skipped = append(p.Skipped, m)
			continue
		}
		inScope := selected == "/" || m.Path == containing.Path || within(root, m.Path)
		if !inScope {
			continue
		}
		if !(selected != "/" && m.Path == containing.Path) && p.Exclusions.Contains(m.Path) {
			m.Eligible = false
			m.SkipReason = SkipExcludedMount
			p.Skipped = append(p.Skipped, m)
			continue
		}
		m.Eligible = true
		candidates = append(candidates, m)
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if selected != "/" {
			as, bs := a.Path == containing.Path, b.Path == containing.Path
			if as != bs {
				return as
			}
		}
		if a.Device != b.Device {
			return a.Device < b.Device
		}
		da, db := pathDepth(a.Root), pathDepth(b.Root)
		if da != db {
			return da < db
		}
		if a.Root != b.Root {
			return a.Root < b.Root
		}
		return a.Path < b.Path
	})
	accepted := make([]Mount, 0, len(candidates))
	rootTries := make(map[Device]*PathTrie)
	for _, m := range candidates {
		trie := rootTries[Device(m.Device)]
		if trie == nil {
			trie = &PathTrie{}
			rootTries[Device(m.Device)] = trie
		}
		if trie.Overlaps(m.Root) {
			m.Eligible = false
			m.SkipReason = SkipBindAlias
			p.Skipped = append(p.Skipped, m)
			continue
		}
		if e := trie.Add(m.Root); e != nil {
			return p, e
		}
		accepted = append(accepted, m)
	}
	p.Roots = accepted
	if selected != "/" {
		found := false
		for _, m := range accepted {
			if m.Path == containing.Path {
				found = true
				break
			}
		}
		if !found {
			return p, fmt.Errorf("selected path %q is on an aliased or unsupported mount", root)
		}
	}
	return p, nil
}

func pathDepth(p string) int {
	if p == "/" {
		return 0
	}
	return strings.Count(p, "/")
}
