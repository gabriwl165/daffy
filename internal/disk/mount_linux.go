//go:build linux

package disk

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type linuxScanner struct{}

func (linuxScanner) Mounts() ([]Mount, error) {
	f, e := os.Open("/proc/self/mountinfo")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return ParseMountInfo(f)
}
func (linuxScanner) Capacity(path string) (MountCapacity, error) { return capacity(path) }

// ParseMountInfo decodes the kernel's octal escapes and retains mount-root identity,
// allowing callers to identify bind aliases by device and filesystem root.
func ParseMountInfo(r interface{ Read([]byte) (int, error) }) ([]Mount, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 1024*1024)
	out := []Mount{}
	for line := 1; s.Scan(); line++ {
		fields := strings.Fields(s.Text())
		sep := -1
		for i, v := range fields {
			if v == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+4 {
			return nil, fmt.Errorf("mountinfo line %d malformed", line)
		}
		root, e := unescapeMount(fields[3])
		if e != nil {
			return nil, fmt.Errorf("mountinfo line %d root: %w", line, e)
		}
		path, e := unescapeMount(fields[4])
		if e != nil {
			return nil, fmt.Errorf("mountinfo line %d mountpoint: %w", line, e)
		}
		source, e := unescapeMount(fields[sep+2])
		if e != nil {
			return nil, fmt.Errorf("mountinfo line %d source: %w", line, e)
		}
		m := Mount{Root: root, Path: path, Device: fields[2], Filesystem: Filesystem(fields[sep+1]), Source: source, Eligible: true}
		if m.Path == "" || m.Path[0] != '/' || filepath.Clean(m.Path) != m.Path || filepath.Clean(m.Root) != m.Root {
			return nil, fmt.Errorf("mountinfo line %d has invalid mountpoint", line)
		}
		if !eligible(m) {
			m.Eligible = false
			m.SkipReason = SkipUnsupportedFilesystem
			m.Reason = "virtual, network, removable, or unsupported filesystem"
		}
		if m.Filesystem == FilesystemOverlay && m.Path != "/" {
			m.Eligible = false
			m.SkipReason = SkipNestedOverlay
			m.Reason = "nested overlay filesystem"
		}
		if removablePath(m.Path) {
			m.Eligible = false
			m.SkipReason = SkipRemovable
			m.Reason = "removable media mount path"
		}
		out = append(out, m)
	}
	if e := s.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
func unescapeMount(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			if i+3 >= len(s) {
				return "", fmt.Errorf("truncated escape")
			}
			if n, e := strconv.ParseUint(s[i+1:i+4], 8, 8); e == nil && (n == 040 || n == 011 || n == 012 || n == 0134) {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
			return "", fmt.Errorf("invalid escape %q", s[i:i+4])
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func newScanner() Scanner { return &scanner{platform: linuxScanner{}} }
