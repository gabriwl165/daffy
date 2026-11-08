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
