package health

import "testing"

func TestDistroDescriptors(t *testing.T) {
	ubuntu := newUbuntuDescriptor()
	zorin := newZorinDescriptor()
	if ubuntu.Name() != "ubuntu" || zorin.Name() != "zorin" {
		t.Fatalf("constructors: %q, %q", ubuntu.Name(), zorin.Name())
	}
	for _, tool := range []string{"journalctl", "systemctl", "systemd-analyze"} {
		if got := ubuntu.Package(tool); got != "systemd" {
			t.Errorf("ubuntu %s package = %q", tool, got)
		}
	}
	for tool, want := range map[string]string{"powerprofilesctl": "power-profiles-daemon", "sensors": "lm-sensors", "lspci": "pciutils", "smartctl": "smartmontools"} {
		if got := zorin.Package(tool); got != want {
			t.Errorf("zorin %s package = %q, want %q", tool, got, want)
		}
	}
	if got := ubuntu.Package("unknown-tool"); got != "" {
		t.Errorf("unknown tool package = %q", got)
	}
}

func TestDistroDescriptorRegistryExtension(t *testing.T) {
	distroRegistry["descriptor-test"] = distroDescriptor{name: "test"}
	defer delete(distroRegistry, "descriptor-test")
	if got := selectDistroAdapter("descriptor-test").Name(); got != "test" {
		t.Fatalf("registered descriptor = %q", got)
	}
	if got := selectDistroAdapter("descriptor-test-extra").Name(); got != "linux" {
		t.Fatalf("non-exact ID selected %q", got)
	}
}
