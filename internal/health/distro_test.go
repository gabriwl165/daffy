package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOSReleaseParsingAndSelection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(p, []byte("ID=zorin\nID_LIKE=ubuntu\nVERSION_ID='17'\nPRETTY_NAME=\"Dollar $HOME \\\"quote\\\" \\\\ path\"\nEVIL=\"$(touch /tmp/nope)\"\nBROKEN=\"bad\"tail\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := parseOSRelease(p)
	if err != nil || m["PRETTY_NAME"] != "Dollar $HOME \"quote\" \\ path" || m["EVIL"] != "$(touch /tmp/nope)" || m["BROKEN"] != "" || selectDistroAdapter(m["ID"]).Name() != "zorin" {
		t.Fatalf("parse/selection: %#v %v", m, err)
	}
	for _, id := range []string{"", "linuxmint", "ubuntu-derivative"} {
		if selectDistroAdapter(id).Name() != "linux" {
			t.Errorf("%q was not generic", id)
		}
	}
	if selectDistroAdapter("ubuntu").Name() != "ubuntu" {
		t.Fatal("ubuntu ID not selected")
	}
	if _, err := parseOSRelease(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestDistroRegistrySupportsAdapter(t *testing.T) {
	distroRegistry["test-distro"] = testDistroAdapter{}
	defer delete(distroRegistry, "test-distro")
	if selectDistroAdapter("test-distro").Name() != "test" {
		t.Fatal("registry extension not selected")
	}
}

type testDistroAdapter struct{}

func (testDistroAdapter) Name() string          { return "test" }
func (testDistroAdapter) Package(string) string { return "" }

func TestDistroCollectAndDoctor(t *testing.T) {
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", oldPath)
	for _, tc := range []struct {
		id, data, want string
		apt            bool
	}{{"ubuntu", "ID=ubuntu\nVERSION_ID=24.04\n", "ubuntu", true}, {"zorin", "ID=zorin\nID_LIKE=ubuntu\n", "zorin", true}, {"derivative", "ID=linuxmint\nID_LIKE=ubuntu\n", "linux", false}, {"missing", "", "linux", false}, {"malformed", "ID=\"broken\n", "linux", false}} {
		d := t.TempDir()
		etc := filepath.Join(d, "etc")
		_ = os.MkdirAll(etc, 0700)
		if tc.data != "" {
			_ = os.WriteFile(filepath.Join(etc, "os-release"), []byte(tc.data), 0600)
		}
		r := CollectCore(context.Background(), Options{EtcRoot: etc, ProcRoot: filepath.Join(d, "proc"), SysRoot: filepath.Join(d, "sys"), SampleInterval: 1})
		if r.System["distro_adapter"] != tc.want {
			t.Errorf("%s core metadata: %#v", tc.id, r.System)
		}
		if tc.id == "ubuntu" && r.System["distro_version"] != "24.04" {
			t.Errorf("version missing: %#v", r.System)
		}
		checks := Doctor(context.Background(), Options{EtcRoot: etc})
		by := map[string]Check{}
		for _, c := range checks {
			by[c.Name] = c
		}
		if by["distro"].Detail != tc.want {
			t.Errorf("%s doctor: %+v", tc.id, by["distro"])
		}
		if strings.Contains(by["journalctl"].Detail, "optional package") != tc.apt {
			t.Errorf("%s guidance: %+v", tc.id, by["journalctl"])
		}
	}
}
