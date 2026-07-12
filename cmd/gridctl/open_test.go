package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestResolveOpenURLDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	url, running, err := resolveOpenURL(0, "", "/")
	if err != nil {
		t.Fatalf("resolveOpenURL: %v", err)
	}
	if url != "http://localhost:8180/" {
		t.Errorf("url = %q, want default port 8180", url)
	}
	if running {
		t.Error("running should be false with no state")
	}
}

func TestResolveOpenURLPortOverrideAndPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	url, _, err := resolveOpenURL(8199, "", "dash")
	if err != nil {
		t.Fatalf("resolveOpenURL: %v", err)
	}
	if url != "http://localhost:8199/dash" {
		t.Errorf("url = %q, want normalized path on port 8199", url)
	}
}

func TestResolveOpenURLRunningStack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := &state.DaemonState{StackName: "demo", StackFile: "/x.yaml", PID: os.Getpid(), Port: 8555, StartedAt: time.Now()}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	url, running, err := resolveOpenURL(0, "", "/")
	if err != nil {
		t.Fatalf("resolveOpenURL: %v", err)
	}
	if url != "http://localhost:8555/" {
		t.Errorf("url = %q, want the running stack's port", url)
	}
	if !running {
		t.Error("running should be true for a live stack")
	}
}

func TestResolveOpenURLUnknownStack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, _, err := resolveOpenURL(0, "ghost", "/")
	if err == nil {
		t.Fatal("expected an error for a stack that is not running")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want not running", err)
	}
}

func TestRunOpenPrintDoesNotLaunchBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	launched := false
	orig := browserOpener
	browserOpener = func(string) error { launched = true; return nil }
	t.Cleanup(func() { browserOpener = orig })

	origPrint, origJSON, origPort, origStack, origPath := openPrint, openJSON, openPort, openStack, openPath
	t.Cleanup(func() {
		openPrint, openJSON, openPort, openStack, openPath = origPrint, origJSON, origPort, origStack, origPath
	})
	openPrint, openJSON, openPort, openStack, openPath = true, false, 0, "", "/"

	var stdout, stderr bytes.Buffer
	if err := runOpen(&stdout, &stderr); err != nil {
		t.Fatalf("runOpen: %v", err)
	}
	if launched {
		t.Error("--print must not launch a browser")
	}
	if !strings.Contains(stdout.String(), "http://localhost:8180/") {
		t.Errorf("stdout = %q, want the URL", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no running gateway") {
		t.Errorf("stderr = %q, want the not-running warning", stderr.String())
	}
}

func TestRunOpenJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := browserOpener
	browserOpener = func(string) error { t.Error("JSON mode must not launch a browser"); return nil }
	t.Cleanup(func() { browserOpener = orig })

	origPrint, origJSON, origPort, origStack, origPath := openPrint, openJSON, openPort, openStack, openPath
	t.Cleanup(func() {
		openPrint, openJSON, openPort, openStack, openPath = origPrint, origJSON, origPort, origStack, origPath
	})
	openPrint, openJSON, openPort, openStack, openPath = false, true, 0, "", "/"

	var stdout, stderr bytes.Buffer
	if err := runOpen(&stdout, &stderr); err != nil {
		t.Fatalf("runOpen: %v", err)
	}
	if !strings.Contains(stdout.String(), `"url": "http://localhost:8180/"`) {
		t.Errorf("stdout = %q, want JSON url field", stdout.String())
	}
}

func TestRunOpenLaunchesBrowser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var opened string
	orig := browserOpener
	browserOpener = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { browserOpener = orig })

	origPrint, origJSON, origPort, origStack, origPath := openPrint, openJSON, openPort, openStack, openPath
	t.Cleanup(func() {
		openPrint, openJSON, openPort, openStack, openPath = origPrint, origJSON, origPort, origStack, origPath
	})
	openPrint, openJSON, openPort, openStack, openPath = false, false, 8123, "", "/"

	var stdout, stderr bytes.Buffer
	if err := runOpen(&stdout, &stderr); err != nil {
		t.Fatalf("runOpen: %v", err)
	}
	if opened != "http://localhost:8123/" {
		t.Errorf("opened = %q, want the resolved URL", opened)
	}
}
