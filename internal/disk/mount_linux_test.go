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

func TestParseMountInfoEscapedBackslashAndInvalidEscape(t *testing.T) {
	line := "36 25 0:32 /root\\134name /mnt/space\\040tab\\011line\\012end rw - ext4 /dev/disk\\134x rw\n"
	ms, e := ParseMountInfo(strings.NewReader(line))
	if e != nil {
		t.Fatal(e)
	}
	if ms[0].Root != "/root\\name" || ms[0].Path != "/mnt/space tab\tline\nend" || ms[0].Source != "/dev/disk\\x" {
		t.Fatalf("decoded mount: %#v", ms[0])
	}
	if _, e = ParseMountInfo(strings.NewReader("36 25 0:32 / /mnt\\999 rw - ext4 /dev/a rw\n")); e == nil {
		t.Fatal("malformed escape accepted")
	}
}
