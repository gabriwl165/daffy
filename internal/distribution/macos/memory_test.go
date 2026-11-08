package macos

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestParseVMStatPageSizesAndSpeculativeSeparation(t *testing.T) {
	for _, size := range []uint64{4096, 16384} {
		input := "Mach Virtual Memory Statistics: (page size of " + fmtUint(size) + " bytes)\nPages free: 2.\nPages speculative: 3.\n"
		free, spec, present, err := ParseVMStat(input)
		if err != nil || free != 2*size || spec != 3*size || !present {
			t.Fatalf("size %d: %d %d %t %v", size, free, spec, present, err)
		}
	}
}

func fmtUint(v uint64) string { return strconv.FormatUint(v, 10) }

func TestParseVMStatRejectsInvalid(t *testing.T) {
	for _, input := range []string{"Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 18446744073709551615.\n", "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 1.\nPages free: 2.\n", "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages speculative: 1.\n", "Mach Virtual Memory Statistics: (page size of 0 bytes)\nPages free: 0.\n"} {
		if _, _, _, err := ParseVMStat(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	if _, _, _, err := ParseVMStat("Pages free: 2.\nMach Virtual Memory Statistics: (page size of 4096 bytes)\n"); err == nil {
		t.Fatal("accepted page count before page-size header")
	}
}

func TestParseSwapUsage(t *testing.T) {
	total, used, free, err := ParseSwapUsage("total = 1024.00M used = 1.00G free = 0.00K (encrypted)")
	if err != nil || total != 1024*1024*1024 || used != 1024*1024*1024 || free != 0 {
		t.Fatalf("%d %d %d %v", total, used, free, err)
	}
	if _, _, _, err = ParseSwapUsage("total = 10M used = 8M free = 0M"); err == nil {
		t.Fatal("accepted inconsistent total")
	}
	for _, input := range []string{"total = 1024M used = 100M free = 924M", "total = 1024.0M used = 100.1M free = 923.8M", "total = 1024.00M used = 100.10M free = 923.90M", "total = 0B used = 0B free = 0B"} {
		if _, _, _, err := ParseSwapUsage(input); err != nil {
			t.Fatalf("rejected rounding-valid %q: %v", input, err)
		}
	}
	if _, _, _, err := ParseSwapUsage("total = 1024.0M used = 100.0M free = 900.0M"); err == nil {
		t.Fatal("accepted large inconsistency")
	}
}

type memoryFake struct {
	results  []DiagnosticResult
	commands []DiagnosticCommand
	cancel   context.CancelFunc
	cancelAt int
}

func (f *memoryFake) Run(ctx context.Context, c DiagnosticCommand) DiagnosticResult {
	f.commands = append(f.commands, c)
	if f.cancel != nil && len(f.commands) == f.cancelAt {
		f.cancel()
	}
	if len(f.results) == 0 {
		return DiagnosticResult{Err: errors.New("missing fake result")}
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r
}

func TestMemoryCollectorIndependentFailuresAndCancellation(t *testing.T) {
	vm := []byte("Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 2.\n")
	swap := []byte("total = 1G used = 512M free = 512M")
	f := &memoryFake{results: []DiagnosticResult{{Stdout: vm}, {Stdout: swap}}}
	u, re, se := (MemoryCollector{f}).Collect(context.Background(), 3*time.Second)
	if re != nil || se != nil || u.ImmediatelyFreeBytes != 8192 || u.SwapTotalBytes != 1<<30 || len(f.commands) != 2 || f.commands[1].Executable != MemorySysctlExecutable {
		t.Fatalf("%+v %v %v %+v", u, re, se, f.commands)
	}
	f = &memoryFake{results: []DiagnosticResult{{Stdout: vm, Stderr: []byte("bad")}, {Stdout: swap}}}
	_, re, se = (MemoryCollector{f}).Collect(context.Background(), time.Second)
	if re == nil || se != nil {
		t.Fatalf("independent errors: %v %v", re, se)
	}
	f = &memoryFake{results: []DiagnosticResult{{Stdout: vm, Err: errors.New("vm failed")}, {Stdout: swap}}}
	_, re, se = (MemoryCollector{f}).Collect(context.Background(), time.Second)
	if re == nil || se != nil {
		t.Fatalf("vm command error lost: %v %v", re, se)
	}
	f = &memoryFake{results: []DiagnosticResult{{Stdout: vm}, {Stdout: swap, Err: errors.New("swap failed")}}}
	_, re, se = (MemoryCollector{f}).Collect(context.Background(), time.Second)
	if re != nil || se == nil {
		t.Fatalf("swap command error lost: %v %v", re, se)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f = &memoryFake{results: []DiagnosticResult{{Stdout: vm}, {Stdout: swap}}, cancel: cancel, cancelAt: 1}
	_, _, se = (MemoryCollector{f}).Collect(ctx, time.Second)
	if !errors.Is(se, context.Canceled) || len(f.commands) != 1 {
		t.Fatalf("cancel: %v calls=%d", se, len(f.commands))
	}
}
