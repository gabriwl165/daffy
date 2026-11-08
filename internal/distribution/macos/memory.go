package macos

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	MemoryVMStatExecutable                  = "/usr/bin/vm_stat"
	MemorySysctlExecutable                  = "/usr/sbin/sysctl"
	MemoryOutputLimit                       = 16 * 1024
	VMPageSizePrefix                        = "page size of "
	VMFreePagesField                        = "Pages free"
	VMSpeculativePagesField                 = "Pages speculative"
	SwapTotalField                          = "total"
	SwapUsedField                           = "used"
	SwapFreeField                           = "free"
	MemorySwapUsageKey      MemorySysctlKey = "vm.swapusage"
	SwapUnitBytes                           = "B"
	SwapUnitKilobytes                       = "K"
	SwapUnitMegabytes                       = "M"
	SwapUnitGigabytes                       = "G"
	SwapUnitTerabytes                       = "T"
)

type MemorySysctlKey string
type swapField uint8

const (
	swapFieldUnknown swapField = iota
	swapFieldTotal
	swapFieldUsed
	swapFieldFree
)

func parseSwapField(key string) swapField {
	switch key {
	case SwapTotalField:
		return swapFieldTotal
	case SwapUsedField:
		return swapFieldUsed
	case SwapFreeField:
		return swapFieldFree
	default:
		return swapFieldUnknown
	}
}

type MemoryUsage struct {
	ImmediatelyFreeBytes    uint64
	SpeculativeBytes        uint64
	SpeculativePagesPresent bool
	SwapTotalBytes          uint64
	SwapUsedBytes           uint64
	SwapFreeBytes           uint64
}

type MemoryCollector struct{ Runner DiagnosticRunner }

type vmField uint8

const (
	vmUnknown vmField = iota
	vmFree
	vmSpeculative
)

func parseVMField(s string) vmField {
	switch s {
	case VMFreePagesField:
		return vmFree
	case VMSpeculativePagesField:
		return vmSpeculative
	default:
		return vmUnknown
	}
}

// Collect reads vm_stat and vm.swapusage independently; failures in either
// command are returned as independent RAM and swap errors.
func (c MemoryCollector) Collect(ctx context.Context, timeout time.Duration) (MemoryUsage, error, error) {
	var usage MemoryUsage
	if c.Runner == nil {
		e := fmt.Errorf("memory diagnostic runner unavailable")
		return usage, e, e
	}
	if err := ctx.Err(); err != nil {
		return usage, err, err
	}
	vm := c.Runner.Run(ctx, DiagnosticCommand{Executable: MemoryVMStatExecutable, Timeout: timeout})
	ramErr := vm.Err
	if ctx.Err() != nil {
		ramErr = ctx.Err()
	} else if vm.Err != nil {
		ramErr = vm.Err
	} else if vm.Truncated || len(vm.Stdout) > MemoryOutputLimit || len(vm.Stderr) != 0 {
		ramErr = fmt.Errorf("vm_stat output invalid or truncated")
	} else {
		usage.ImmediatelyFreeBytes, usage.SpeculativeBytes, usage.SpeculativePagesPresent, ramErr = ParseVMStat(string(vm.Stdout))
	}
	if ctx.Err() != nil {
		return usage, ramErr, ctx.Err()
	}
	swap := c.Runner.Run(ctx, DiagnosticCommand{Executable: MemorySysctlExecutable, Args: []string{"-n", string(MemorySwapUsageKey)}, Timeout: timeout})
	swapErr := swap.Err
	if ctx.Err() != nil {
		swapErr = ctx.Err()
	} else if swap.Err != nil {
		swapErr = swap.Err
	} else if swap.Truncated || len(swap.Stdout) > MemoryOutputLimit || len(swap.Stderr) != 0 {
		swapErr = fmt.Errorf("swap output invalid or truncated")
	} else {
		usage.SwapTotalBytes, usage.SwapUsedBytes, usage.SwapFreeBytes, swapErr = ParseSwapUsage(string(swap.Stdout))
	}
	return usage, ramErr, swapErr
}

func ParseVMStat(output string) (freeBytes, speculativeBytes uint64, speculativePresent bool, err error) {
	if len(output) > MemoryOutputLimit {
		return 0, 0, false, fmt.Errorf("vm_stat output exceeds limit")
	}
	var pageSize uint64
	var pageSeen, freeSeen, specSeen bool
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, VMPageSizePrefix) {
			if pageSeen {
				return 0, 0, false, fmt.Errorf("duplicate page size")
			}
			_, rest, _ := strings.Cut(line, VMPageSizePrefix)
			rest = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(rest), ")"), " bytes")
			n, e := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
			if e != nil || n == 0 {
				return 0, 0, false, fmt.Errorf("invalid page size")
			}
			pageSize, pageSeen = n, true
			continue
		}
		fields := strings.SplitN(line, ":", 2)
		if len(fields) != 2 {
			continue
		}
		field := parseVMField(strings.TrimSpace(fields[0]))
		if field == vmUnknown {
			continue
		}
		if !pageSeen {
			return 0, 0, false, fmt.Errorf("page size must precede page counts")
		}
		if field == vmFree && freeSeen || field == vmSpeculative && specSeen {
			return 0, 0, false, fmt.Errorf("duplicate vm_stat field")
		}
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(fields[1]), "."))
		pages, e := strconv.ParseUint(value, 10, 64)
		if e != nil {
			return 0, 0, false, fmt.Errorf("invalid page count")
		}
		if field == vmFree {
			freeSeen = true
			if pages != 0 && pageSize > math.MaxUint64/pages {
				return 0, 0, false, fmt.Errorf("free page byte count overflow")
			}
			freeBytes = pages * pageSize
		} else {
			specSeen = true
			if pages != 0 && pageSize > math.MaxUint64/pages {
				return 0, 0, false, fmt.Errorf("speculative page byte count overflow")
			}
			speculativeBytes = pages * pageSize
		}
	}
	if !pageSeen || !freeSeen {
		return 0, 0, false, fmt.Errorf("required vm_stat fields missing")
	}
	return freeBytes, speculativeBytes, specSeen, nil
}

func ParseSwapUsage(output string) (total, used, free uint64, err error) {
	if len(output) > MemoryOutputLimit {
		return 0, 0, 0, fmt.Errorf("swap output exceeds limit")
	}
	var totalSeen, usedSeen, freeSeen bool
	var rounding uint64
	fields := strings.Fields(strings.TrimSpace(output))
	for i := 0; i < len(fields); i++ {
		key := strings.TrimSuffix(fields[i], "=")
		valueToken := ""
		if strings.HasSuffix(fields[i], "=") && i+1 < len(fields) {
			i++
			valueToken = fields[i]
		} else if i+2 < len(fields) && fields[i+1] == "=" {
			i += 2
			valueToken = fields[i]
		} else {
			continue
		}
		var dest *uint64
		var seen *bool
		switch parseSwapField(key) {
		case swapFieldTotal:
			dest, seen = &total, &totalSeen
		case swapFieldUsed:
			dest, seen = &used, &usedSeen
		case swapFieldFree:
			dest, seen = &free, &freeSeen
		default:
			continue
		}
		if *seen {
			return 0, 0, 0, fmt.Errorf("duplicate swap field")
		}
		value, tolerance, e := parseSwapBytes(valueToken)
		if e != nil {
			return 0, 0, 0, e
		}
		*dest, *seen = value, true
		if tolerance > rounding {
			rounding = tolerance
		}
	}
	if !totalSeen || !usedSeen || !freeSeen {
		return 0, 0, 0, fmt.Errorf("required swap fields missing")
	}
	if used > total || free > total {
		return 0, 0, 0, fmt.Errorf("swap values inconsistent")
	}
	if free > math.MaxUint64-used {
		return 0, 0, 0, fmt.Errorf("swap values overflow")
	}
	sum := used + free
	tolerance := rounding
	if tolerance <= math.MaxUint64/3 {
		tolerance *= 3
	} else {
		tolerance = math.MaxUint64
	}
	if (sum > total && sum-total > tolerance) || (total > sum && total-sum > tolerance) {
		return 0, 0, 0, fmt.Errorf("swap totals inconsistent")
	}
	return total, used, free, nil
}

func parseSwapBytes(s string) (uint64, uint64, error) {
	if len(s) < 2 {
		return 0, 0, fmt.Errorf("malformed swap size")
	}
	suffix := strings.ToUpper(s[len(s)-1:])
	power := uint(0)
	switch suffix {
	case SwapUnitBytes:
		power = 0
	case SwapUnitKilobytes:
		power = 10
	case SwapUnitMegabytes:
		power = 20
	case SwapUnitGigabytes:
		power = 30
	case SwapUnitTerabytes:
		power = 40
	default:
		return 0, 0, fmt.Errorf("unknown swap unit")
	}
	numeric := s[:len(s)-1]
	n, e := strconv.ParseFloat(numeric, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, 0, fmt.Errorf("invalid swap size")
	}
	unit := uint64(1) << power
	bytes := n * float64(unit)
	if bytes >= float64(math.MaxUint64) {
		return 0, 0, fmt.Errorf("swap size overflow")
	}
	precision := 0
	if dot := strings.IndexByte(numeric, '.'); dot >= 0 {
		precision = len(numeric) - dot - 1
		if precision > 9 {
			return 0, 0, fmt.Errorf("excess swap precision")
		}
	}
	denominator := uint64(2)
	for i := 0; i < precision; i++ {
		denominator *= 10
	}
	return uint64(math.Round(bytes)), unit / denominator, nil
}
