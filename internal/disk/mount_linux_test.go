//go:build linux

package disk

import (
	"strings"
	"testing"
)

func TestParseMountInfoEscapesAndEligibility(t *testing.T) {
	ms, e := ParseMountInfo(strings.NewReader("36 25 0:32 / /mnt/a\\040b rw,relatime - ext4 /dev/sda rw\n37 25 0:33 / /proc rw - proc proc rw\n"))
	if e != nil {
		t.Fatal(e)
	}
	if len(ms) != 2 || ms[0].Path != "/mnt/a b" || !ms[0].Eligible || ms[1].Eligible {
		t.Fatalf("unexpected mounts: %#v", ms)
	}
}
func TestParseMountInfoMalformed(t *testing.T) {
	if _, e := ParseMountInfo(strings.NewReader("bad\n")); e == nil {
		t.Fatal("expected error")
	}
}
