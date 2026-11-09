package docker

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gabriwl165/daffy/internal/plugins"
)

func (p *Plugin) runtimeSamples(ctx context.Context, opts plugins.Options, d *plugins.DockerDetails, o *plugins.Outcome) error {
	n := opts.Samples
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 60 || opts.Interval < 0 || opts.Interval > time.Minute || n > 1 && opts.Interval < 100*time.Millisecond || opts.RuntimeTop < 0 || opts.RuntimeTop > 100 || invalidPercent(opts.CPUThreshold) || invalidPercent(opts.MemoryThreshold) || opts.CPUThreshold < 0 || opts.MemoryThreshold < 0 || opts.MemoryThreshold > 100 {
		return plugins.ErrInvalid
	}
	if opts.RuntimeSort != "" && opts.RuntimeSort != plugins.DockerRuntimeSortCPU && opts.RuntimeSort != plugins.DockerRuntimeSortMemory {
		return plugins.ErrInvalid
	}
	peaks := map[string]*plugins.DockerRuntimeTrendItem{}
	completed := 0
	requested := opts.Samples != 0 || opts.Interval != 0 || opts.RuntimeTop != 0 || opts.CPUThreshold != 0 || opts.MemoryThreshold != 0 || opts.RuntimeSort != ""
	memory := []memoryAccumulator{}
	defer func() {
		if requested && d.RuntimeTrend == nil {
			d.RuntimeTrend = makeTrend(peaks, completed, opts)
		}
		if opts.MemoryDetails {
			d.Memory = finishMemory(memory)
		}
	}()
	for i := 0; i < n; i++ {
		if i > 0 {
			t := time.NewTimer(opts.Interval)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		candidate := &plugins.DockerDetails{Context: d.Context}
		candidateOutcome := &plugins.Outcome{Status: plugins.StatusComplete}
		if err := p.runtime(ctx, candidate, candidateOutcome); err != nil {
			o.Issues = append(o.Issues, candidateOutcome.Issues...)
			o.Findings = append(o.Findings, candidateOutcome.Findings...)
			if candidateOutcome.Status != "" && candidateOutcome.Status != plugins.StatusComplete {
				o.Status = candidateOutcome.Status
			}
			return err
		}
		needsValidation := requested || opts.MemoryDetails
		if needsValidation {
			if err := validateRuntimePercentages(candidate.Runtime); err != nil {
				fail(o, issueRuntimeInvalid)
				return err
			}
		}
		// Only a complete, validated stats response becomes observable.
		d.Runtime = candidate.Runtime
		if opts.MemoryDetails {
			threshold := opts.MemoryThreshold
			if threshold == 0 {
				threshold = sustainedMemoryPressureThreshold
			}
			recordMemory(&memory, candidate.Runtime, threshold)
		}
		if !requested {
			if opts.MemoryDetails {
				if err := p.memoryInspect(ctx, candidate.Runtime, memory); err != nil {
					fail(o, issueInspectUnavailable)
					return err
				}
			}
			continue
		}
		type observation struct {
			row plugins.DockerContainerStats
			cpu float64
			mem float64
		}
		observations := make([]observation, 0, len(candidate.Runtime))
		for _, row := range candidate.Runtime {
			cpu, e1 := percent(row.CPUPerc)
			mem, e2 := percent(row.MemPerc)
			if e1 != nil || e2 != nil || invalidPercent(cpu) || invalidPercent(mem) || cpu < 0 || mem < 0 || mem > 100 {
				fail(o, issueRuntimeInvalid)
				return ErrInvalidResponse
			}
			observations = append(observations, observation{row: row, cpu: cpu, mem: mem})
		}
		// A sample contributes only after every row has validated.
		for _, sample := range observations {
			row, cpu, mem := sample.row, sample.cpu, sample.mem
			item := peaks[row.ID]
			if item == nil {
				item = &plugins.DockerRuntimeTrendItem{ID: row.ID, Name: row.Name}
				peaks[row.ID] = item
			}
			item.Name = row.Name
			if cpu > item.PeakCPUPercent {
				item.PeakCPUPercent = cpu
			}
			if mem > item.PeakMemoryPercent {
				item.PeakMemoryPercent = mem
			}
			if bytes, _, ok := parseMemUsage(row.MemUsage); ok && bytes > item.PeakMemoryBytes {
				item.PeakMemoryBytes = bytes
			}
		}
		completed++
		if opts.MemoryDetails {
			if err := p.memoryInspect(ctx, candidate.Runtime, memory); err != nil {
				fail(o, issueInspectUnavailable)
				return err
			}
		}
		// A missing container in a sample is churn, not a zero reading.
	}
	if requested {
		d.RuntimeTrend = makeTrend(peaks, completed, opts)
	}
	if opts.MemoryDetails {
		d.Memory = finishMemory(memory)
	}
	if opts.RuntimeSort == plugins.DockerRuntimeSortMemory {
		sort.SliceStable(d.Runtime, func(i, j int) bool {
			a, _, oka := parseMemUsage(d.Runtime[i].MemUsage)
			b, _, okb := parseMemUsage(d.Runtime[j].MemUsage)
			if oka != okb {
				return oka
			}
			return a > b
		})
	}
	return nil
}

func validateRuntimePercentages(rows []plugins.DockerContainerStats) error {
	for _, row := range rows {
		cpu, e1 := percent(row.CPUPerc)
		mem, e2 := percent(row.MemPerc)
		if e1 != nil || e2 != nil || invalidPercent(cpu) || invalidPercent(mem) || cpu < 0 || mem < 0 || mem > 100 {
			return ErrInvalidResponse
		}
	}
	return nil
}

type memoryAccumulator struct {
	item plugins.DockerMemoryDiagnostic
}

const sustainedMemoryPressureThreshold = 80.0

func recordMemory(items *[]memoryAccumulator, rows []plugins.DockerContainerStats, threshold float64) {
	for _, row := range rows {
		current, _, ok := parseMemUsage(row.MemUsage)
		if !ok {
			continue
		}
		idx := -1
		for i := range *items {
			if (*items)[i].item.ID == row.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			*items = append(*items, memoryAccumulator{item: plugins.DockerMemoryDiagnostic{ID: row.ID, Name: row.Name, Denominator: plugins.DockerMemoryDenominatorUnknown}})
			idx = len(*items) - 1
		}
		a := &(*items)[idx]
		a.item.Name = row.Name
		if a.item.FirstBytes == nil {
			v := current
			a.item.FirstBytes = &v
		}
		v := current
		a.item.CurrentBytes = &v
		if a.item.PeakBytes == nil || current > *a.item.PeakBytes {
			x := current
			a.item.PeakBytes = &x
		}
		a.item.SampleCount++
		pressure, err := percent(row.MemPerc)
		if err == nil && !invalidPercent(pressure) && pressure >= threshold {
			a.item.PressureSampleCount++
		}
		if err == nil && !invalidPercent(pressure) {
			a.item.PressureObservationCount++
			a.item.PressureThresholdPercent = threshold
		}
	}
}

func finishMemory(items []memoryAccumulator) []plugins.DockerMemoryDiagnostic {
	out := make([]plugins.DockerMemoryDiagnostic, 0, len(items))
	for _, a := range items {
		item := a.item
		if item.FirstBytes != nil && item.CurrentBytes != nil {
			delta, ok := signedDelta(*item.CurrentBytes, *item.FirstBytes)
			if ok {
				item.ObservedGrowthBytes = &delta
				item.RisingUsage = delta > 0
			}
		}
		item.SustainedPressure = item.PressureObservationCount > 1 && item.PressureSampleCount >= 2 && item.PressureSampleCount*2 >= item.PressureObservationCount
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func signedDelta(a, b uint64) (int64, bool) {
	const max = uint64(1<<63 - 1)
	if a >= b {
		d := a - b
		if d > max {
			return 0, false
		}
		return int64(d), true
	}
	d := b - a
	if d > max+1 {
		return 0, false
	}
	if d == max+1 {
		return -1 << 63, true
	}
	return -int64(d), true
}

func (p *Plugin) memoryInspect(ctx context.Context, current []plugins.DockerContainerStats, samples []memoryAccumulator) error {
	ids := make([]string, 0, len(current))
	for _, row := range current {
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	for i := range samples {
		if contains(ids, samples[i].item.ID) {
			samples[i].item.LimitBytes = nil
			samples[i].item.HeadroomBytes = nil
			samples[i].item.OOMKilled = nil
			samples[i].item.Denominator = plugins.DockerMemoryDenominatorUnknown
		}
	}
	for start := 0; start < len(ids); start += inspectBatch {
		end := start + inspectBatch
		if end > len(ids) {
			end = len(ids)
		}
		b, err := p.call(ctx, "docker inspect", append([]string{"container", "inspect"}, ids[start:end]...)...)
		if err != nil {
			return err
		}
		var rows []struct {
			ID   string `json:"Id"`
			Host *struct {
				Memory *int64 `json:"Memory"`
			} `json:"HostConfig"`
			State *struct {
				OOMKilled *bool `json:"OOMKilled"`
			} `json:"State"`
		}
		if json.Unmarshal(b, &rows) != nil {
			return ErrInvalidResponse
		}
		seen := []string{}
		for _, r := range rows {
			id := strings.TrimPrefix(r.ID, "sha256:")
			if !validID(id) || contains(seen, id) || !contains(ids[start:end], id) {
				return ErrInvalidResponse
			}
			seen = append(seen, id)
			for i := range samples {
				if samples[i].item.ID != id {
					continue
				}
				a := &samples[i]
				if r.Host != nil && r.Host.Memory != nil && *r.Host.Memory > 0 {
					v := uint64(*r.Host.Memory)
					a.item.LimitBytes = &v
					a.item.Denominator = plugins.DockerMemoryDenominatorContainerLimit
					if a.item.CurrentBytes != nil && *a.item.CurrentBytes <= v {
						h := v - *a.item.CurrentBytes
						a.item.HeadroomBytes = &h
					} else if a.item.CurrentBytes != nil {
						h := uint64(0)
						a.item.HeadroomBytes = &h
					}
				} else if r.Host != nil && r.Host.Memory != nil && *r.Host.Memory == 0 {
					a.item.Denominator = plugins.DockerMemoryDenominatorDaemonHost
				} else {
					a.item.Denominator = plugins.DockerMemoryDenominatorUnknown
				}
				if r.State != nil {
					a.item.OOMKilled = r.State.OOMKilled
				}
				break
			}
		}
		if len(seen) != end-start {
			return ErrInvalidResponse
		}
	}
	return nil
}

func parseMemUsage(s string) (uint64, uint64, bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, ok1 := parseBytes(parts[0])
	b, ok2 := parseBytes(parts[1])
	return a, b, ok1 && ok2
}

func parseBytes(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		power  uint
	}{{"KiB", 1}, {"MiB", 2}, {"GiB", 3}, {"TiB", 4}, {"kB", 1}, {"KB", 1}, {"MB", 2}, {"GB", 3}, {"TB", 4}, {"B", 0}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, e := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), 64)
			if e != nil || invalidPercent(n) || n < 0 {
				return 0, false
			}
			base := 1024.0
			if u.suffix == "kB" || u.suffix == "KB" || u.suffix == "MB" || u.suffix == "GB" || u.suffix == "TB" {
				base = 1000
			}
			factor := math.Pow(base, float64(u.power))
			v := n * factor
			if invalidPercent(v) || v >= math.Exp2(64) {
				return 0, false
			}
			return uint64(v), true
		}
	}
	return 0, false
}

func makeTrend(peaks map[string]*plugins.DockerRuntimeTrendItem, completed int, opts plugins.Options) *plugins.DockerRuntimeTrend {
	items := make([]plugins.DockerRuntimeTrendItem, 0, len(peaks))
	for _, p := range peaks {
		p.CPUThresholdExceeded = opts.CPUThreshold > 0 && p.PeakCPUPercent >= opts.CPUThreshold
		p.MemoryThresholdExceeded = opts.MemoryThreshold > 0 && p.PeakMemoryPercent >= opts.MemoryThreshold
		items = append(items, *p)
	}
	sort.Slice(items, func(i, j int) bool {
		if opts.RuntimeSort == plugins.DockerRuntimeSortMemory && items[i].PeakMemoryBytes != items[j].PeakMemoryBytes {
			return items[i].PeakMemoryBytes > items[j].PeakMemoryBytes
		}
		if items[i].PeakCPUPercent != items[j].PeakCPUPercent {
			return items[i].PeakCPUPercent > items[j].PeakCPUPercent
		}
		if items[i].PeakMemoryPercent != items[j].PeakMemoryPercent {
			return items[i].PeakMemoryPercent > items[j].PeakMemoryPercent
		}
		return items[i].ID < items[j].ID
	})
	if opts.RuntimeTop > 0 && len(items) > opts.RuntimeTop {
		items = items[:opts.RuntimeTop]
	}
	return &plugins.DockerRuntimeTrend{SampleCount: completed, Containers: items}
}

func percent(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
}

func invalidPercent(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) }
