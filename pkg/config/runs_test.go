package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRunsConfig_AbsentIsDisabled(t *testing.T) {
	var s Stack
	if err := yaml.Unmarshal([]byte("version: \"1\"\nname: t\nmcp-servers: []\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runs != nil {
		t.Fatalf("expected nil Runs, got %+v", s.Runs)
	}
	s.SetDefaults()
	if s.Runs != nil {
		t.Fatal("SetDefaults must not synthesize a runs block")
	}
}

func TestRunsConfig_EnabledDefaults(t *testing.T) {
	var s Stack
	if err := yaml.Unmarshal([]byte("version: \"1\"\nname: t\nruns:\n  enabled: true\nmcp-servers: []\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.Runs == nil || !s.Runs.Enabled {
		t.Fatal("expected enabled runs")
	}
	s.SetDefaults()
	if s.Runs.Retention == nil || s.Runs.Retention.MaxSizeMB != 100 || s.Runs.Retention.MaxAgeDays != 7 {
		t.Fatalf("defaults = %+v", s.Runs.Retention)
	}
}

func TestRunsConfig_DisabledDoesNotFillRetention(t *testing.T) {
	s := Stack{Name: "t", Runs: &RunsConfig{Enabled: false}}
	s.SetDefaults()
	if s.Runs.Retention != nil {
		t.Fatal("disabled runs should not fill retention")
	}
}

func TestValidateRunsRetention(t *testing.T) {
	s := &Stack{Name: "t", Network: Network{Name: "t-net"}, Runs: &RunsConfig{Enabled: true, Retention: &RunsRetention{MaxSizeMB: 0, MaxAgeDays: 7}}}
	if err := Validate(s); err == nil {
		t.Fatal("expected invalid max_size_mb")
	}
	s.Runs.Retention.MaxSizeMB = 100
	s.Runs.Retention.MaxAgeDays = 0
	if err := Validate(s); err == nil {
		t.Fatal("expected invalid max_age_days")
	}
	s.Runs.Retention.MaxAgeDays = 7
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestRunsConfig_DoesNotInheritTelemetry(t *testing.T) {
	s := &Stack{
		Name: "t",
		Telemetry: &TelemetryConfig{
			Persist: TelemetryPersistence{Logs: true, Metrics: true, Traces: true},
		},
	}
	if s.Runs != nil {
		t.Fatal("telemetry must not enable runs")
	}
}
