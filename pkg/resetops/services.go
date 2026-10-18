package resetops

import (
	"context"
	"fmt"
	"runtime"

	"github.com/gridctl/gridctl/pkg/service"
)

// NewServiceResetter builds the reset adapter for installed user services.
// It returns nil on platforms where the service command is unsupported.
func NewServiceResetter(home string) ServiceResetter {
	if home == "" || runtime.GOOS == "windows" {
		return nil
	}
	return serviceResetter{m: service.NewManager(home, service.NewSupervisor())}
}

type serviceResetter struct {
	m *service.Manager
}

func (s serviceResetter) Preview(ctx context.Context, force bool) ([]Row, error) {
	rows, err := s.m.Preview(ctx, force)
	if err != nil {
		return nil, err
	}
	return convertServiceRows(rows, true), nil
}

func (s serviceResetter) Remove(ctx context.Context, force bool) ([]Row, error) {
	rows, err := s.m.Remove(ctx, force)
	if err != nil {
		return nil, err
	}
	return convertServiceRows(rows, false), nil
}

func convertServiceRows(rows []service.ResetRow, preview bool) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		action := r.Action
		if preview {
			switch action {
			case "removed", "already-gone", "dropped-record":
				action = ActionWouldRemove
			}
		}
		if action == "failed" {
			action = ActionFailed
		}
		out = append(out, Row{
			Kind:   "service",
			Name:   r.Name,
			Client: r.Client,
			Path:   r.Path,
			Action: action,
			Detail: r.Detail,
			Error:  r.Error,
		})
	}
	return out
}

func serviceStillInstalled(ctx context.Context, home, name string) (bool, error) {
	if home == "" || name == "" {
		return false, nil
	}
	entry, err := service.Lookup(ctx, home, name)
	if err != nil {
		return false, fmt.Errorf("reading service record for %s: %w", name, err)
	}
	return entry != nil, nil
}
