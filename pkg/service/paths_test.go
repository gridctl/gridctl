package service

import (
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"netops":    "netops",
		"my stack":  "my-stack",
		"a---b":     "a-b",
		"...foo...": "foo",
		"Foo_1.2":   "Foo_1.2",
		"***":       "",
		"a/b":       "a-b",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnitPath_LinuxCombinations(t *testing.T) {
	home := "/tmp/demo"
	t.Run("default", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := UnitPath(home, false, PlatformSystemd, "netops")
		want := "/tmp/demo/.config/systemd/user/gridctl-netops.service"
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
	t.Run("xdg honored when home is not overridden", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/elsewhere/config")
		got := UnitPath(home, false, PlatformSystemd, "netops")
		want := "/elsewhere/config/systemd/user/gridctl-netops.service"
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
	t.Run("xdg inside overridden home", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/demo/.config")
		got := UnitPath(home, true, PlatformSystemd, "netops")
		want := "/tmp/demo/.config/systemd/user/gridctl-netops.service"
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
	t.Run("xdg outside overridden home is ignored", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/home/real/.config")
		got := UnitPath(home, true, PlatformSystemd, "netops")
		want := "/tmp/demo/.config/systemd/user/gridctl-netops.service"
		if got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})
}

func TestUnitPath_LaunchdIgnoresXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/elsewhere/config")
	got := UnitPath("/Users/u", true, PlatformLaunchd, "netops")
	want := "/Users/u/Library/LaunchAgents/dev.gridctl.netops.plist"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestLabel(t *testing.T) {
	if got := Label("my stack"); got != "dev.gridctl.my-stack" {
		t.Fatalf("Label = %s", got)
	}
}
