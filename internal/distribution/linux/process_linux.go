package linux

import (
	"context"
	"fmt"
	"github.com/gabriwl165/daffy/internal/distribution/process"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const tcpListenState = "0A"
const socketLinkPrefix = "socket:["

type linuxFinder struct{ procRoot string }

func NewProcessFinder(root string) process.Finder { return linuxFinder{root} }

func (f linuxFinder) Find(ctx context.Context, t process.ProcessTarget) ([]process.ProcessOwner, error) {
	if err := process.ValidateTarget(t); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.Kind == process.TargetPID {
		if _, err := os.Stat(filepath.Join(f.procRoot, strconv.Itoa(t.PID))); err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: err}
		}
		identity, err := readStartTime(filepath.Join(f.procRoot, strconv.Itoa(t.PID), "stat"))
		if err != nil {
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: err}
		}
		return []process.ProcessOwner{{PID: t.PID, Identity: identity}}, nil
	}
	var inodes []string
	for _, name := range []string{"net/tcp", "net/tcp6"} {
		b, err := os.ReadFile(filepath.Join(f.procRoot, name))
		if err != nil {
			if name == "net/tcp6" && os.IsNotExist(err) {
				continue
			}
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: err}
		}
		found, err := parseTCPInodes(string(b), t.Port)
		if err != nil {
			return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: err}
		}
		for _, inode := range found {
			if !contains(inodes, inode) {
				inodes = append(inodes, inode)
			}
		}
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(f.procRoot)
	if err != nil {
		return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: err}
	}
	var owners []process.ProcessOwner
	incomplete := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fdDir := filepath.Join(f.procRoot, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			incomplete = true
			continue
		}
		found := false
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				incomplete = true
				continue
			}
			if strings.HasPrefix(target, socketLinkPrefix) && contains(inodes, strings.TrimSuffix(strings.TrimPrefix(target, socketLinkPrefix), "]")) {
				found = true
				break
			}
		}
		if found {
			identity, e := readStartTime(filepath.Join(f.procRoot, entry.Name(), "stat"))
			if e != nil {
				incomplete = true
				continue
			}
			owners = append(owners, process.ProcessOwner{PID: pid, Identity: identity})
		}
	}
	if incomplete {
		return nil, &process.ProcessError{Kind: process.ProcessDiscoveryFailed, Err: fmt.Errorf("incomplete /proc fd discovery")}
	}
	return owners, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func readStartTime(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	return parseStartTime(string(b))
}

func parseStartTime(stat string) (string, error) {
	open := strings.Index(stat, " (")
	if open <= 0 {
		return "", fmt.Errorf("malformed proc stat pid/comm")
	}
	if _, err := strconv.Atoi(stat[:open]); err != nil {
		return "", fmt.Errorf("malformed proc stat pid: %w", err)
	}
	close := strings.LastIndex(stat, ")")
	if close < open+2 {
		return "", fmt.Errorf("malformed proc stat comm")
	}
	fields := strings.Fields(stat[close+1:])
	if len(fields) <= 19 {
		return "", fmt.Errorf("proc stat missing starttime")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return "", fmt.Errorf("invalid proc stat starttime")
	}
	return strconv.FormatUint(start, 10), nil
}

const tcpHeaderPrefix = "sl local_address rem_address st"
const tcp6HeaderPrefix = "sl local_address remote_address st"

func parseTCPInodes(table string, port int) ([]string, error) {
	lines := strings.Split(table, "\n")
	header := false
	var inodes []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !header {
			prefix := strings.Join(fields[:min(len(fields), 4)], " ")
			if prefix != tcpHeaderPrefix && prefix != tcp6HeaderPrefix {
				return nil, fmt.Errorf("malformed tcp table header")
			}
			header = true
			continue
		}
		if len(fields) < 10 {
			return nil, fmt.Errorf("malformed tcp table row")
		}
		addr := strings.Split(fields[1], ":")
		if len(addr) != 2 || len(addr[0]) == 0 {
			return nil, fmt.Errorf("malformed tcp local address")
		}
		p, err := strconv.ParseUint(addr[1], 16, 16)
		if err != nil {
			return nil, fmt.Errorf("malformed tcp local port")
		}
		if fields[3] != tcpListenState {
			continue
		}
		if int(p) == port {
			inode := fields[9]
			if inode == "" {
				return nil, fmt.Errorf("missing tcp inode")
			}
			if !contains(inodes, inode) {
				inodes = append(inodes, inode)
			}
		}
	}
	if !header {
		return nil, fmt.Errorf("missing tcp table header")
	}
	return inodes, nil
}
