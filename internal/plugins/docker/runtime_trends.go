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
		completed++
		// A missing container in a sample is churn, not a zero reading.
	}
	if requested {
		d.RuntimeTrend = makeTrend(peaks, completed, opts)
	}
	return nil
}

func makeTrend(peaks map[string]*plugins.DockerRuntimeTrendItem, completed int, opts plugins.Options) *plugins.DockerRuntimeTrend {
	items := make([]plugins.DockerRuntimeTrendItem, 0, len(peaks))
	for _, p := range peaks {
		p.CPUThresholdExceeded = opts.CPUThreshold > 0 && p.PeakCPUPercent >= opts.CPUThreshold
		p.MemoryThresholdExceeded = opts.MemoryThreshold > 0 && p.PeakMemoryPercent >= opts.MemoryThreshold
		items = append(items, *p)
	}
	sort.Slice(items, func(i, j int) bool {
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
