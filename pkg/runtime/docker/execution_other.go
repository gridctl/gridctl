//go:build !linux

package docker

import (
	"context"
	"fmt"

	"github.com/gridctl/gridctl/pkg/execution"
)

func observeExecution(ctx context.Context, _ string, _ int, _ *execution.ExecutionContract) ([]execution.Control, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("execution.runtime: trusted daemon-host observation unsupported on this platform")
}
