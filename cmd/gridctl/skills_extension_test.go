package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skillsext"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/stretchr/testify/require"
)

func TestStatus_SkillsExtension(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	t.Setenv(gatewayTokenEnv, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			_, _ = fmt.Fprintf(w, `{"code_mode":"on","extensions":[%q]}`, mcp.SkillsExtensionID)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	require.NoError(t, state.Save(&state.DaemonState{StackName: "skills", Port: port, PID: os.Getpid()}))
	mode, _, _, extensions, _, warning := queryGatewayStatus(port)
	require.Equal(t, "on", mode)
	require.Empty(t, warning)
	require.Equal(t, []string{mcp.SkillsExtensionID}, extensions)
	stdout, _ := captureOutput(t, func() error { return runStatus("skills", false, true, true) })
	require.Contains(t, stdout, `"extensions"`)
	require.Contains(t, stdout, mcp.SkillsExtensionID)
}

func TestSkillList_ManifestSummary(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	oldFormat, oldRemote := skillListFormat, skillListRemote
	t.Cleanup(func() { skillListFormat, skillListRemote = oldFormat, oldRemote })
	skillListFormat, skillListRemote = "json", false
	store := registry.NewStore(registryDir())
	sk := &registry.AgentSkill{Name: "alpha", Description: "Alpha", State: registry.StateActive, Body: "Instructions"}
	require.NoError(t, store.SaveSkill(sk))
	require.NoError(t, store.WriteFile("alpha", "helper.py", []byte("print('alpha')")))
	manifest, err := skillpins.ComputeManifest(context.Background(), store, sk)
	require.NoError(t, err)
	stdout, _ := captureOutput(t, runSkillList)
	var entries []struct {
		Name     string                    `json:"name"`
		Manifest skillsext.ManifestSummary `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "alpha", entries[0].Name)
	require.Equal(t, skillsext.Summarize(manifest), entries[0].Manifest)
}

func TestSkillList_UnreadableManifestKeepsOtherSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode 000 is not a reliable deny on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000")
	}
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	oldFormat, oldRemote := skillListFormat, skillListRemote
	t.Cleanup(func() { skillListFormat, skillListRemote = oldFormat, oldRemote })
	skillListFormat, skillListRemote = "json", false
	store := registry.NewStore(registryDir())
	require.NoError(t, store.SaveSkill(&registry.AgentSkill{Name: "alpha", Description: "Alpha", State: registry.StateActive, Body: "Alpha"}))
	require.NoError(t, store.SaveSkill(&registry.AgentSkill{Name: "beta", Description: "Beta", State: registry.StateActive, Body: "Beta"}))
	require.NoError(t, store.WriteFile("alpha", "scripts/locked.sh", []byte("echo locked\n")))
	locked := filepath.Join(registryDir(), "skills", "alpha", "scripts", "locked.sh")
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	stdout, _ := captureOutput(t, runSkillList)
	var entries []struct {
		Name          string                     `json:"name"`
		Manifest      *skillsext.ManifestSummary `json:"manifest"`
		ManifestError string                     `json:"manifest_error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &entries))
	require.Len(t, entries, 2)
	byName := map[string]struct {
		Manifest      *skillsext.ManifestSummary
		ManifestError string
	}{}
	for _, entry := range entries {
		byName[entry.Name] = struct {
			Manifest      *skillsext.ManifestSummary
			ManifestError string
		}{entry.Manifest, entry.ManifestError}
	}
	require.Equal(t, "digest_unavailable", byName["alpha"].ManifestError)
	require.Nil(t, byName["alpha"].Manifest)
	require.Empty(t, byName["beta"].ManifestError)
	require.NotNil(t, byName["beta"].Manifest)
	require.NotEmpty(t, byName["beta"].Manifest.Digest)
	require.NotContains(t, stdout, "warning:")

	skillListFormat = ""
	human, _ := captureOutput(t, runSkillList)
	require.Contains(t, human, "alpha")
	require.Contains(t, human, "beta")
	require.Contains(t, human, "warning: skill alpha manifest could not be read (digest_unavailable)")
}
