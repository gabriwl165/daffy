package docker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gabriwl165/daffy/internal/plugins"
)

// Empty envelope fixture mirrors Docker CLI v29.1.3 formatter disk_usage.go
// and reflect.go (https://github.com/docker/cli/blob/v29.1.3/cli/command/formatter/disk_usage.go).
const emptyDiskEnvelope = `{"Images":[],"Containers":[],"Volumes":[],"BuildCache":[]}`

func TestDiskDetailsEnvelopeAndSafeFields(t *testing.T) {
	verbose := `{"Images":[{"ID":"img","Repository":"repo","Tag":"latest","Size":"N/A","SharedSize":"N/A","UniqueSize":"3MB","Containers":"1","Labels":"SECRET"}],"Containers":[{"ID":"cid","Names":"app","Image":"repo:latest","Size":"N/A","LocalVolumes":"0","Status":"Up","Mounts":"SECRET"}],"Volumes":[{"Name":"vol","Links":"0","Size":"N/A","Mountpoint":"SECRET"}],"BuildCache":[{"ID":"cache*","CacheType":"regular","Size":"2MB","UsageCount":"0","InUse":"false","Shared":"false","Secret":"SECRET"}]}`
	for _, fixture := range []string{emptyDiskEnvelope, verbose} {
		r := &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"1","Active":"1","Size":"3MB","Reclaimable":"0B"}`), []byte(fixture)}}
		o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{DiskDetails: true}})
		if err != nil || len(r.args) != 2 || len(o.Details.Docker.DiskUsage) != 1 || len(o.Details.Docker.DiskBreakdown) != 4 {
			t.Fatalf("details %v %#v", err, o)
		}
		encoded, _ := json.Marshal(o)
		if strings.Contains(string(encoded), "SECRET") {
			t.Fatal("unknown sensitive fields leaked")
		}
		if fixture == verbose && o.Details.Docker.DiskBreakdown[0].Items[0].Size != "N/A" {
			t.Fatal("human size was not retained")
		}
	}
}

func TestDiskDetailsRejectsUnsupportedEnvelope(t *testing.T) {
	for _, data := range []string{`{"Images":[],"Containers":[],"Volumes":[]}`, `not json`, `{"Images":null,"Containers":[],"Volumes":[],"BuildCache":[]}`} {
		r := &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"0","Active":"0","Size":"0B","Reclaimable":"0B"}`), []byte(data)}}
		o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{DiskDetails: true}})
		if err == nil || o.Status != plugins.StatusPartial {
			t.Errorf("data %s => %#v %v", data, o, err)
		}
	}
}

func TestDiskDetailsUsesSelectedContextForBothCalls(t *testing.T) {
	r := &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"0","Active":"0","Size":"0B","Reclaimable":"0B"}`), []byte(emptyDiskEnvelope)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{Context: "dev", DiskDetails: true}})
	if err != nil || len(r.args) != 2 || len(o.Details.Docker.DiskUsage) != 1 || len(o.Details.Docker.DiskBreakdown) != 4 {
		t.Fatalf("context details: %#v %v calls=%#v", o, err, r.args)
	}
	for i, args := range r.args {
		if !containsArg(args, "--context") || !containsArg(args, "dev") {
			t.Errorf("call %d missing context: %#v", i, args)
		}
	}
}

func TestDiskDetailsRejectsMalformedRowsAndRetainsSummary(t *testing.T) {
	fixtures := []string{`{"Images":[{}],"Containers":[],"Volumes":[],"BuildCache":[]}`, `{"Images":[{"ID":"i"}],"Containers":[],"Volumes":[],"BuildCache":[]}`, `{"Images":[],"Containers":[null],"Volumes":[],"BuildCache":[]}`, `{"Images":[],"Containers":[],"Volumes":[{"Name":"v"}],"BuildCache":[]}`, `{"Images":[],"Containers":[],"Volumes":[],"BuildCache":[{"ID":"c","Size":" "}]}`}
	for _, data := range fixtures {
		r := &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"2","Active":"1","Size":"4MB","Reclaimable":"N/A"}`), []byte(data)}}
		o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{DiskDetails: true}})
		if err == nil || o.Status != plugins.StatusPartial || len(o.Details.Docker.DiskUsage) != 1 || o.Details.Docker.DiskUsage[0].TotalCount != 2 {
			t.Errorf("malformed row lost summary: %s => %#v %v", data, o, err)
		}
	}
}

func TestImageDiagnosticsFilterSortUnknownAndHostUnavailable(t *testing.T) {
	const env = `{"Images":[{"ID":"a","Repository":"z","Tag":"latest","Size":"2GB","UniqueSize":"1GB","Containers":"1"},{"ID":"b","Repository":"<none>","Tag":"<none>","Size":"bad","UniqueSize":"N/A","Containers":"N/A"},{"ID":"c","Repository":"a","Tag":"tag","Size":"1GB","UniqueSize":"500MB","Containers":"0"}],"Containers":[],"Volumes":[],"BuildCache":[]}`
	makeRunner := func() *fakeRunner {
		return &fakeRunner{outputs: [][]byte{[]byte(`{"Type":"Images","TotalCount":"3","Active":"1","Size":"3GB","Reclaimable":"0B"}`), []byte(env)}}
	}
	for _, tc := range []struct {
		filter plugins.DockerImageFilter
		count  int
		id     string
	}{{plugins.DockerImageFilterReferenced, 1, "a"}, {plugins.DockerImageFilterUnused, 1, "c"}, {plugins.DockerImageFilterDangling, 1, "b"}} {
		o, err := New(makeRunner()).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{ImageFilter: tc.filter}})
		if err != nil || len(o.Details.Docker.Images) != tc.count || o.Details.Docker.Images[0].ID != tc.id {
			t.Fatalf("filter %s: %#v %v", tc.filter, o, err)
		}
	}
	oldProbe := probeFilesystem
	probeFilesystem = func(root, endpoint string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
		if root != "/local/root" || endpoint != "/var/run/docker.sock" {
			t.Errorf("probe inputs %q %q", root, endpoint)
		}
		return filesystemCapacity{}, plugins.DockerHostReasonUnverified
	}
	defer func() { probeFilesystem = oldProbe }()
	r := makeRunner()
	r.outputs = [][]byte{[]byte(`"unix:///var/run/docker.sock"`), []byte(`"/local/root"`), []byte(`{"Type":"Images","TotalCount":"3","Active":"1","Size":"3GB","Reclaimable":"0B"}`), []byte(env)}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{DiskSort: plugins.DockerDiskSortSize, HostCapacity: true}})
	if err != nil || len(o.Details.Docker.Images) != 3 || o.Details.Docker.Images[0].ID != "a" || o.Details.Docker.Images[1].ID != "c" || o.Details.Docker.Images[2].SizeBytes != nil {
		t.Fatalf("sort/parse: %#v %v", o, err)
	}
	storage := o.Details.Docker.Storage
	if storage == nil || !storage.SharedLayers || storage.SizeTotalsComparable || storage.HostCapacity == nil || storage.HostCapacity.Available || storage.HostCapacity.UnavailableReason == "" {
		t.Fatalf("storage safety: %#v", storage)
	}
}

func TestHostCapacityInjectedProbeAndPressure(t *testing.T) {
	old := probeFilesystem
	defer func() { probeFilesystem = old }()
	probeFilesystem = func(string, string) (filesystemCapacity, plugins.DockerHostUnavailableReason) {
		return filesystemCapacity{total: 1000, free: 100, available: 150, inodesTotal: 100, inodesFree: 5}, ""
	}
	r := &fakeRunner{outputs: [][]byte{[]byte(`"unix:///tmp/docker.sock"`), []byte(`"/var/lib/docker"`), []byte(`{"Type":"Images","TotalCount":"0","Active":"0","Size":"0B","Reclaimable":"0B"}`), []byte(emptyDiskEnvelope)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{HostCapacity: true}})
	if err != nil {
		t.Fatal(err)
	}
	h := o.Details.Docker.Storage.HostCapacity
	if !h.Available || h.LowBytes || !h.LowInodes || *h.BytesAvailable != 150 || *h.InodesFree != 5 {
		t.Fatalf("capacity: %#v", h)
	}
}

func TestDockerByteParserRejectsNonFiniteAndOverflow(t *testing.T) {
	for _, value := range []string{"NaN GB", "+Inf GB", "-1 GB", "9223372036854775808 B", "not-a-size"} {
		if got := parseDockerBytes(value); got != nil {
			t.Errorf("%q parsed as %d", value, *got)
		}
	}
}

func TestRemoteHostCapacityIsExplicitlyUnavailable(t *testing.T) {
	r := &fakeRunner{outputs: [][]byte{[]byte(`"tcp://daemon.example:2376"`), []byte(`{"Type":"Images","TotalCount":"0","Active":"0","Size":"0B","Reclaimable":"0B"}`), []byte(emptyDiskEnvelope)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationDisk, Options: plugins.Options{Context: "remote", HostCapacity: true}})
	if err != nil {
		t.Fatal(err)
	}
	h := o.Details.Docker.Storage.HostCapacity
	if h == nil || h.Available || h.UnavailableReason != plugins.DockerHostReasonRemote || len(r.args) != 3 {
		t.Fatalf("remote host claim: %#v calls=%d", h, len(r.args))
	}
}
