package docker

import (
	"context"
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
	peaks := map[string]*plugins.DockerRuntimeTrendItem{}
	completed := 0
	requested := opts.Samples != 0 || opts.Interval != 0 || opts.RuntimeTop != 0 || opts.CPUThreshold != 0 || opts.MemoryThreshold != 0
	memory := map[string]*plugins.DockerMemoryDiagnostic{}
	defer func() {
		if requested && d.RuntimeTrend == nil {
			d.RuntimeTrend = makeTrend(peaks, completed, opts)
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
		if err := p.runtime(ctx, d, o); err != nil {
			return err
		}
		if !requested {
			if opts.MemoryDetails {
				recordMemory(memory, d.Runtime)
			}
			continue
		}
		type observation struct {
			row plugins.DockerContainerStats
			cpu float64
			mem float64
		}
		observations := make([]observation, 0, len(d.Runtime))
		for _, row := range d.Runtime {
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
		}
		if opts.MemoryDetails {
			recordMemory(memory, d.Runtime)
		}
		completed++
		// A missing container in a sample is churn, not a zero reading.
	}
	if requested {
		d.RuntimeTrend = makeTrend(peaks, completed, opts)
	}
	if opts.MemoryDetails {
		d.Memory = finishMemory(memory, completed)
	}
	return nil
}

func recordMemory(items map[string]*plugins.DockerMemoryDiagnostic, rows []plugins.DockerContainerStats) {
	for _, row := range rows {
		current, peak, ok := parseMemUsage(row.MemUsage)
		item := items[row.ID]
		if item == nil {
			item = &plugins.DockerMemoryDiagnostic{ID: row.ID, Name: row.Name, Denominator: plugins.DockerMemoryDenominatorUnknown}
			items[row.ID] = item
		}
		item.Name = row.Name
		if !ok {
			continue
		}
		if item.FirstBytes == nil {
			v := current
			item.FirstBytes = &v
		}
		if item.CurrentBytes == nil || current > *item.CurrentBytes {
			v := current
			item.PeakBytes = &v
		}
		v := current
		item.CurrentBytes = &v
		if item.PeakBytes == nil || peak > *item.PeakBytes {
			v := peak
			item.PeakBytes = &v
		}
		item.SampleCount++
	}
}

func finishMemory(items map[string]*plugins.DockerMemoryDiagnostic, _ int) []plugins.DockerMemoryDiagnostic {
	out := make([]plugins.DockerMemoryDiagnostic, 0, len(items))
	for _, item := range items {
		if item.FirstBytes != nil && item.CurrentBytes != nil {
			delta := int64(0)
			if *item.CurrentBytes >= *item.FirstBytes {
				delta = int64(*item.CurrentBytes - *item.FirstBytes)
			} else {
				delta = -int64(*item.FirstBytes - *item.CurrentBytes)
			}
			item.ObservedGrowthBytes = &delta
			item.RisingUsage = delta > 0
		}
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
			factor := math.Pow(1024, float64(u.power))
			v := n * factor
			if invalidPercent(v) || v > float64(^uint64(0)) {
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
		if opts.RuntimeSort == plugins.DockerRuntimeSortMemory && items[i].PeakMemoryPercent != items[j].PeakMemoryPercent {
			return items[i].PeakMemoryPercent > items[j].PeakMemoryPercent
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
