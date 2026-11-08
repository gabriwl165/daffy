package health

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportJSONModelContract(t *testing.T) {
	r := Report{System: SystemInfo{OS: "Linux", Kernel: "6.1", DistroID: "test", DistroAdapter: "linux"}, Findings: []Finding{{ID: "x", Area: AreaCPU, Severity: SeverityWarning, Summary: "test", Confidence: ConfidenceMedium}}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"os":"Linux"`, `"kernel":"6.1"`, `"distro_id":"test"`, `"distro_adapter":"linux"`, `"area":"cpu"`, `"severity":"warning"`, `"confidence":"medium"`} {
		if !strings.Contains(s, key) {
			t.Errorf("missing JSON contract %s in %s", key, s)
		}
	}
	for _, key := range []string{`"cpu_model"`, `"distro_version"`, `"evidence"`} {
		if strings.Contains(s, key) {
			t.Errorf("unexpected optional field %s in %s", key, s)
		}
	}
}
