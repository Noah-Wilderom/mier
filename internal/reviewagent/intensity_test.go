package reviewagent

import "testing"

func TestModelEffortRespectsLiveCapabilities(t *testing.T) {
	models := []Model{{ID: "flexible", Default: true, ReasoningEfforts: []string{"low", "medium", "high"}}, {ID: "fixed", ReasoningEfforts: []string{"medium"}}, {ID: "plain"}}
	for _, test := range []struct{ model, intensity, expected string }{
		{"flexible", "high", "high"}, {"", "low", "low"}, {"fixed", "high", ""}, {"fixed", "medium", "medium"}, {"plain", "high", ""}, {"missing", "high", ""},
	} {
		if actual := ModelEffort(models, test.model, test.intensity); actual != test.expected {
			t.Fatalf("%s/%s: got %q, want %q", test.model, test.intensity, actual, test.expected)
		}
	}
	if value, err := NormalizeIntensity(""); err != nil || value != "medium" {
		t.Fatal("older preferences need the balanced default")
	}
	if _, err := NormalizeIntensity("extreme"); err == nil {
		t.Fatal("unsupported intensity accepted")
	}
}
