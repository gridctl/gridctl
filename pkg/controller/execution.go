package controller

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

type executionChecker interface {
	CheckExecution(context.Context, string, *execution.ExecutionContract) (*execution.Report, error)
	CheckExecutionBeforeStart(context.Context, string, *execution.ExecutionContract) error
}

func wireExecution(cfg *mcp.MCPServerConfig, server config.MCPServer, rt runtime.WorkloadRuntime, id string) {
	cfg.Execution = server.Execution
	contract, resolveErr := config.ResolveExecution(server)
	cfg.ExecutionRequested = execution.RequestedReport(contract)
	if server.Execution == nil || !server.IsContainerBased() {
		return
	}
	cfg.ContainerID = id
	cfg.ExecutionBeforeStart = func(ctx context.Context) error {
		if resolveErr != nil {
			return resolveErr
		}
		checker, ok := rt.(executionChecker)
		if !ok {
			return fmt.Errorf("execution: runtime admission unavailable")
		}
		return checker.CheckExecutionBeforeStart(ctx, id, contract)
	}
	cfg.ExecutionCheck = func(ctx context.Context) (*execution.Report, error) {
		if resolveErr != nil {
			return nil, resolveErr
		}
		checker, ok := rt.(executionChecker)
		if !ok {
			return nil, fmt.Errorf("execution: runtime admission unavailable")
		}
		report, err := checker.CheckExecution(ctx, id, contract)
		if err != nil || cfg.Transport == mcp.TransportStdio {
			return report, err
		}
		endpoint, parseErr := url.Parse(cfg.Endpoint)
		var port int
		if parseErr == nil {
			port, parseErr = strconv.Atoi(endpoint.Port())
		}
		if parseErr != nil || report == nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || port != report.EndpointPort {
			if report != nil {
				report.Eligible, report.Outcome = false, "mismatch"
			}
			failure := fmt.Errorf("execution.endpoint: MCP endpoint does not match the inspected replica")
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			if err := rt.Stop(cleanupCtx, runtime.WorkloadID(id)); err != nil {
				return report, errors.Join(failure, fmt.Errorf("execution.cleanup: endpoint mismatch stop failed"))
			}
			return report, failure
		}
		return report, nil
	}
}
