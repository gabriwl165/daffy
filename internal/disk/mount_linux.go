//go:build linux

package disk

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type linuxScanner struct{}

func newScanner() Scanner { return &scanner{platform: linuxScanner{}} }
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
		m := Mount{Root: unescapeMount(fields[3]), Path: unescapeMount(fields[4]), Device: fields[2], Filesystem: fields[sep+1], Source: unescapeMount(fields[sep+2]), Eligible: true}
		if m.Path == "" || m.Path[0] != '/' {
			return nil, fmt.Errorf("mountinfo line %d has invalid mountpoint", line)
		}
		switch m.Filesystem {
		case "proc", "sysfs", "devpts", "devtmpfs", "cgroup", "cgroup2", "securityfs", "debugfs", "tracefs", "configfs", "pstore", "mqueue", "fusectl", "rpc_pipefs", "squashfs", "nfs", "nfs4", "cifs", "smb3", "9p", "autofs":
			m.Eligible = false
			m.Reason = "virtual, network, or read-only image filesystem"
		}
		for _, opt := range strings.Split(fields[5], ",") {
			if opt == "ro" {
				m.Eligible = false
				m.Reason = "read-only filesystem"
			}
		}
		out = append(out, m)
	}
	if e := s.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, e := strconv.ParseUint(s[i+1:i+4], 8, 8); e == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
