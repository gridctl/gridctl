package resetops

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
)

type fakeServiceResetter struct {
	preview []Row
	removed []Row
}

func (f fakeServiceResetter) Preview(context.Context, bool) ([]Row, error) {
	return f.preview, nil
}

func (f fakeServiceResetter) Remove(context.Context, bool) ([]Row, error) {
	return f.removed, nil
}

func TestPreview_ListsService(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{Home: home, Services: fakeServiceResetter{preview: []Row{{
		Kind: "service", Name: "netops", Client: "systemd", Path: "/unit", Action: ActionWouldRemove,
	}}}}
	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range doc.Rows {
		if row.Kind == "service" && row.Name == "netops" && row.Action == ActionWouldRemove {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %+v", doc.Rows)
	}
}

func TestExecute_DoesNotKillSupervisedDaemon(t *testing.T) {
	home := sandboxHome(t)
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	m := &Managers{Home: home, Services: fakeServiceResetter{removed: []Row{{
		Kind: "service", Name: "netops", Client: "systemd", Action: ActionKeptDrift,
	}}}}
	// The record is still on disk, which is what the daemon phase checks.
	mgr := service.NewManager(home, nil)
	if _, err := mgr.Install(context.Background(), service.InstallOptions{
		Spec: service.Spec{
			Platform: service.PlatformSystemd, Name: "netops", StackName: "netops",
			Label: "dev.gridctl.netops", Binary: "/bin/gridctl",
			Argv:    []string{"/bin/gridctl", "apply", "/stack.yaml", "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"},
			Env:     []service.KV{{Key: "GRIDCTL_HOME", Value: home}, {Key: "PATH", Value: "/bin"}},
			Home:    home, UnitPath: home + "/gridctl-netops.service", StackFile: "/stack.yaml",
			Port: 8180, BasePort: 9000,
		},
		NoEnable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "netops", PID: cmd.Process.Pid, Port: 9, StartedAt: time.Now(), Home: home,
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsRunning(&state.DaemonState{PID: cmd.Process.Pid}) {
		t.Fatal("supervised daemon was killed")
	}
	skipped := false
	for _, row := range doc.Rows {
		if row.Kind == "daemon" && row.Name == "netops" && row.Action == ActionSkipped {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("daemon row was not skipped: %+v", doc.Rows)
	}
}
