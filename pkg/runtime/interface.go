package runtime

import (
	"context"
	"errors"
)

// WorkloadID uniquely identifies a workload across runtimes.
// For Docker this is a container ID, for K8s a pod UID, for processes a PID.
type WorkloadID string

// WorkloadType identifies the kind of workload.
type WorkloadType string

const (
	WorkloadTypeMCPServer WorkloadType = "mcp-server"
	WorkloadTypeAgent     WorkloadType = "agent"
	WorkloadTypeResource  WorkloadType = "resource"
)

// WorkloadState represents the current state of a workload.
type WorkloadState string

const (
	WorkloadStateRunning  WorkloadState = "running"
	WorkloadStateStopped  WorkloadState = "stopped"
	WorkloadStateFailed   WorkloadState = "failed"
	WorkloadStateCreating WorkloadState = "creating"
	WorkloadStateUnknown  WorkloadState = "unknown"
)

// WorkloadConfig is the runtime-agnostic configuration for starting a workload.
type WorkloadConfig struct {
	// Identity
	Name  string       // Logical name (e.g., "postgres", "weather-server")
	Stack string       // Stack this workload belongs to
	Type  WorkloadType // Type of workload

	// Image/artifact
	Image string // Container image or artifact reference

	// Execution
	Command []string          // Override entrypoint/command
	Env     map[string]string // Environment variables

	// Networking
	NetworkName string // Network to join
	ExposedPort int    // Port the workload exposes (0 if none)
	HostPort    int    // Desired host port (0 for auto-assign)

	// Storage
	Volumes []string // Volume mounts (format: "host:container" or "host:container:mode")

	// Transport-specific
	Transport string // "http", "stdio", "sse"

	// Labels for identification and filtering
	Labels map[string]string
}

// WorkloadStatus is the runtime-agnostic status of a running workload.
type WorkloadStatus struct {
	// Identity
	ID    WorkloadID   // Runtime-assigned unique identifier
	Name  string       // Logical name from config
	Stack string       // Stack name
	Type  WorkloadType // Type of workload

	// State
	State   WorkloadState // Running, Stopped, Failed, etc.
	Message string        // Human-readable status message (e.g., "Up 5 minutes")

	// Networking
	Endpoint string // How to reach this workload (e.g., "localhost:9000")
	HostPort int    // Actual host port (if port-mapped)

	// Metadata
	Image  string            // Image/artifact that's running
	Labels map[string]string // All labels
}

// WorkloadFilter for querying workloads.
type WorkloadFilter struct {
	Stack  string            // Filter by stack name
	Labels map[string]string // Additional label filters
}

// NetworkOptions for network creation.
type NetworkOptions struct {
	Driver string // Network driver (e.g., "bridge")
	Stack  string // For labeling/cleanup purposes
}

// WorkloadRuntime is the interface for managing workload lifecycles.
// Implementations include Docker, Kubernetes, local processes, etc.
type WorkloadRuntime interface {
	// Start starts a workload and returns its status.
	Start(ctx context.Context, cfg WorkloadConfig) (*WorkloadStatus, error)

	// Stop stops a running workload by its ID.
	Stop(ctx context.Context, id WorkloadID) error

	// Remove removes a stopped workload.
	Remove(ctx context.Context, id WorkloadID) error

	// Status returns the current status of a workload.
	Status(ctx context.Context, id WorkloadID) (*WorkloadStatus, error)

	// Exists checks if a workload exists by name.
	Exists(ctx context.Context, name string) (exists bool, id WorkloadID, err error)

	// List returns all workloads matching the filter.
	List(ctx context.Context, filter WorkloadFilter) ([]WorkloadStatus, error)

	// GetHostPort returns the host port for a workload's exposed port.
	GetHostPort(ctx context.Context, id WorkloadID, exposedPort int) (int, error)

	// EnsureNetwork creates the network if it doesn't exist.
	EnsureNetwork(ctx context.Context, name string, opts NetworkOptions) error

	// ListNetworks returns all managed networks for a stack.
	ListNetworks(ctx context.Context, stack string) ([]string, error)

	// RemoveNetwork removes a network by name.
	RemoveNetwork(ctx context.Context, name string) error

	// EnsureImage ensures the image is available locally.
	EnsureImage(ctx context.Context, imageName string) error

	// Ping checks if the runtime is accessible.
	Ping(ctx context.Context) error

	// Close releases runtime resources.
	Close() error
}

// Sentinel errors for runtime operations.
var (
	ErrWorkloadNotFound   = errors.New("workload not found")
	ErrNetworkNotFound    = errors.New("network not found")
	ErrRuntimeUnavailable = errors.New("runtime unavailable")
	ErrNotSupported       = errors.New("operation not supported by this runtime")
	ErrInvalidConfig      = errors.New("invalid workload configuration")
)
