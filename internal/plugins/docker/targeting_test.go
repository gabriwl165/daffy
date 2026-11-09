package docker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gabriwl165/daffy/internal/plugins"
)

const targetID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const otherID = "0123456789abcdef1123456789abcdef0123456789abcdef0123456789abcdef"

func targetListing(ids ...string) []byte {
	var b strings.Builder
	for i, id := range ids {
		name := "api"
		if i > 0 {
			name = "worker"
		}
		b.WriteString("{\"ID\":\"" + id + "\",\"Names\":\"" + name + "\"}\n")
	}
	return []byte(b.String())
}

func TestContainerSelectorResolutionAndMalformedListing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		data     []byte
		selector string
		wantErr  bool
		count    int
	}{
		{"exact name", targetListing(targetID), "api", false, 1},
		{"id prefix", targetListing(targetID), targetID[:12], false, 1},
		{"ambiguous prefix", targetListing(targetID, otherID), targetID[:12], true, 0},
		{"unknown", targetListing(targetID), "none", true, 0},
		{"empty", []byte(""), "api", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{outputs: [][]byte{tc.data}}
			got, err := New(r).selectContainers(context.Background(), []string{tc.selector})
			if (err != nil) != tc.wantErr || len(got) != tc.count {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
	if _, err := New(&fakeRunner{outputs: [][]byte{[]byte("not-json")}}).selectContainers(context.Background(), nil); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("accepted malformed listing: %v", err)
	}
}

func TestTargetedRuntimeUsesOnlySelectedRunningIDs(t *testing.T) {
	r := &fakeRunner{outputs: [][]byte{targetListing(targetID), []byte("")}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Containers: []string{"api"}}})
	if err != nil || r.calls != 2 || len(o.Details.Docker.Runtime) != 0 {
		t.Fatalf("stopped result calls=%d outcome=%#v err=%v", r.calls, o, err)
	}
	for _, args := range r.args {
		if len(args) > 0 && args[0] == "stats" {
			t.Fatal("stats called for stopped-only selection")
		}
	}
}

func TestContextScopesEveryCommandWithoutConcurrentBleed(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]string{}
	r := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		name := args[1]
		mu.Lock()
		seen[name] = append(seen[name], strings.Join(args, " "))
		mu.Unlock()
		switch args[2] {
		case "ps":
			if containsArg(args, "--all") {
				return targetListing(targetID), nil
			}
			return []byte(targetID + "\n"), nil
		case "stats":
			return []byte(`{"ID":"` + targetID + `","Name":"api","CPUPerc":"1%","MemUsage":"1/2","MemPerc":"50%","NetIO":"0/0","BlockIO":"0/0","PIDs":"1"}`), nil
		}
		return nil, nil
	})
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Context: n, Containers: []string{"api"}}})
			if err != nil || o.Status != plugins.StatusComplete {
				t.Errorf("%s: %#v %v", n, o, err)
			}
		}(name)
	}
	wg.Wait()
	for _, name := range []string{"one", "two"} {
		if len(seen[name]) != 3 {
			t.Fatalf("%s commands: %v", name, seen[name])
		}
		for _, argv := range seen[name] {
			if !strings.HasPrefix(argv, "--context "+name+" ") {
				t.Fatalf("context leaked: %q", argv)
			}
		}
	}
}

func TestTargetSelectorsRejectedForDiskAndDefaultBundle(t *testing.T) {
	p := New(&fakeRunner{})
	for _, req := range []plugins.Request{{Operation: plugins.OperationDisk, Options: plugins.Options{Containers: []string{"api"}}}, {Operation: plugins.OperationDisk, Options: plugins.Options{Containers: []string{"api"}, Context: "ctx"}}} {
		if _, err := p.Analyze(context.Background(), req); !errors.Is(err, plugins.ErrInvalid) {
			t.Errorf("accepted %#v: %v", req, err)
		}
	}
}

func TestMalformedContainerListIsPartial(t *testing.T) {
	o, err := New(&fakeRunner{outputs: [][]byte{[]byte("not-json")}}).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if err == nil || o.Status != plugins.StatusPartial || len(o.Issues) == 0 {
		t.Fatalf("malformed listing reported healthy: %#v err=%v", o, err)
	}
}

func TestProjectZeroMatchesSkipsStatsAndPropagatesConfigIssues(t *testing.T) {
	inspect := `[{"Id":"` + targetID + `","Name":"/api","State":{"Status":"exited"},"HostConfig":{"Memory":1,"Privileged":false},"Config":{"Labels":{"com.docker.compose.project":"shop"}}}]`
	r := &fakeRunner{outputs: [][]byte{targetListing(targetID), targetListing(targetID), []byte(inspect)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Project: "missing"}})
	if err != nil || r.calls != 3 || len(o.Details.Docker.Runtime) != 0 {
		t.Fatalf("zero-project result: calls=%d outcome=%#v err=%v", r.calls, o, err)
	}
	for _, args := range r.args {
		if len(args) > 0 && args[0] == "stats" {
			t.Fatal("stats ran for empty project selection")
		}
	}

	badInspect := `[{"Id":"` + targetID + `","Name":"/api","State":{"Status":"running"},"Config":{"Labels":{"com.docker.compose.project":"shop"}}}]`
	r = &fakeRunner{outputs: [][]byte{targetListing(targetID), targetListing(targetID), []byte(badInspect)}}
	o, err = New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationRuntime, Options: plugins.Options{Project: "shop"}})
	if err == nil || o.Status != plugins.StatusPartial || len(o.Issues) == 0 || o.Issues[0].Code != issueHostConfigUnknown {
		t.Fatalf("project config evidence dropped: %#v err=%v", o, err)
	}
}

func TestStandaloneContainerAndUntrustedLabelsRemainPrivate(t *testing.T) {
	inspect := `[{"Id":"` + targetID + `","Name":"/solo","State":{"Status":"running"},"HostConfig":{"Memory":1,"Privileged":false},"Config":{"Labels":{"untrusted":"private-label"}}}]`
	r := &fakeRunner{outputs: [][]byte{targetListing(targetID), []byte(inspect)}}
	o, err := New(r).Analyze(context.Background(), plugins.Request{Operation: plugins.OperationConfig})
	if err != nil || len(o.Details.Docker.Config) != 1 || len(o.Details.Docker.ComposeGroups) != 0 {
		t.Fatalf("standalone container lost: %#v %v", o, err)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "untrusted") || strings.Contains(string(b), "private-label") {
		t.Fatalf("raw label leaked: %s", b)
	}
}
