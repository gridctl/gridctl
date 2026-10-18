package service

import "testing"

func TestParseSystemctlShow(t *testing.T) {
	sample := "ActiveState=active\nSubState=running\nMainPID=41233\nUnitFileState=enabled\nExecMainStartTimestamp=Sun 2026-10-18 10:00:00 UTC\n"
	got := ParseSystemctlShow(sample)
	if got.State != "running" || !got.Enabled || got.PID != 41233 {
		t.Fatalf("got %+v", got)
	}
	failed := ParseSystemctlShow("ActiveState=failed\nSubState=failed\nMainPID=0\nUnitFileState=enabled\n")
	if failed.State != "failed" || !failed.Enabled {
		t.Fatalf("failed = %+v", failed)
	}
	inactive := ParseSystemctlShow("ActiveState=inactive\nSubState=dead\nUnitFileState=disabled\n")
	if inactive.State != "inactive" || inactive.Enabled {
		t.Fatalf("inactive = %+v", inactive)
	}
	missing := ParseSystemctlShow("LoadState=not-found\nActiveState=inactive\nUnitFileState=not-found\n")
	if missing.State != "not-loaded" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	sample := "gui/501/dev.gridctl.netops = {\n\tstate = running\n\tpid = 99\n\tpath = /Users/u/Library/LaunchAgents/dev.gridctl.netops.plist\n}\n"
	got := ParseLaunchctlPrint(sample)
	if got.State != "running" || got.PID != 99 || !got.Enabled {
		t.Fatalf("got %+v", got)
	}
	unknown := ParseLaunchctlPrint("some future format with no known fields\n")
	if unknown.State != "unknown" {
		t.Fatalf("unknown = %+v", unknown)
	}
	missing := ParseLaunchctlPrint("Could not find service \"dev.gridctl.netops\" in domain for user gui: 501\n")
	if missing.State != "not-loaded" {
		t.Fatalf("missing = %+v", missing)
	}
}

func TestParseLinger(t *testing.T) {
	if got := ParseLinger("Linger=yes\n"); got != "yes" {
		t.Fatalf("got %s", got)
	}
	if got := ParseLinger("Linger=no\n"); got != "no" {
		t.Fatalf("got %s", got)
	}
	if got := ParseLinger("nope\n"); got != "unknown" {
		t.Fatalf("got %s", got)
	}
}
