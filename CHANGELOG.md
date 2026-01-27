# Changelog

All notable changes to gridctl will be documented in this file.

## [0.1.0-alpha.3] - 2026-01-27


### Bug Fixes


- Remove duplicate v prefix from gateway node version display
- Wait for MCP servers to initialize before returning from deploy
- Remove changelog generation from release workflow

### Features


- Add ASCII banner with two-tone coloring
- Add colored CLI help with Obsidian Observatory theme
- Display banner on version command
- Add SetVersion method to gateway
- Pass version to gateway on deploy
- Add brand logo asset
- Replace header icon with brand logo
- Add ToolSelector type for agent-level tool filtering
- Add tool whitelist filtering to HTTP MCP client
- Add tool whitelist filtering to stdio MCP client
- Add tool whitelist filtering to process MCP client
- Add agent-level tool filtering to gateway
- Return full ToolSelector in agent status API
- Pass tool whitelist to MCP servers on deploy
- Add tool filtering example
- Add ToolSelector type to frontend
- Add whitelist filtering to ToolList component
- Add Access section to agent sidebar
- Add amber color theme for terminal output
- Add output package with printer and banner
- Add summary tables for workloads and gateways
- Use output package in deploy command

### Refactoring


- Update mergeEquippedSkills for ToolSelector type
- Update validation for ToolSelector type
- Update compat types for ToolSelector
- Update orchestrator for ToolSelector type
- Update graph transform for ToolSelector
- Use output package in version command
- Use output package in status command
- Use output package in destroy command## [0.1.0-alpha.2] - 2026-01-23


### Refactoring


- Update module path to github.com/gridctl/gridctl
- Rename cmd/agentlab to cmd/gridctl
- Update import paths and branding in Go packages
- Update web UI branding to Gridctl## [0.1.0-alpha.1] - 2026-01-21


### Bug Fixes


- Correct handle positions and remove translate-y hover
- Remove translate-y hover to prevent clipping
- Remove translate-y hover to prevent clipping
- Add overflow visible to prevent React Flow clipping
- Position agents on right side of gateway
- Check json decode errors in A2A handler tests
- Add volume mount support to ContainerConfig
- Pass volumes from Resource config to container
- Add SSE response parsing and session tracking to MCP client
- Correct Itential MCP server transport configuration
- Use json.RawMessage for MCP tool input schema
- Serialize A2A skill input schema to json.RawMessage
- Use Record<string, unknown> for tool inputSchema
- Handle generic inputSchema in ToolList component
- Check error return from Process.Kill
- Handle write error in health endpoint
- Change tool name delimiter from :: to __ for client compatibility
- Skip SSE notifications when parsing tool call responses
- Return friendly message for nodes without container logs
- Add liveness health check and readiness endpoint
- Start HTTP server before MCP registration
- Correct tool name delimiter to match backend

### Features


- Add topology configuration types
- Add topology YAML loader
- Add topology validation rules
- Add Docker client interface for mocking
- Add Docker client wrapper
- Add container naming and labels
- Add Docker network management
- Add Docker image pulling
- Add container lifecycle management
- Add high-level runtime orchestration
- Add daemon state management
- Add MCP protocol types and JSON-RPC
- Add HTTP transport MCP client
- Add stdio transport MCP client
- Add MCP session management
- Add MCP tool routing with prefixes
- Add MCP protocol bridge gateway
- Add MCP HTTP request handlers
- Add SSE server for MCP clients
- Add image builder types
- Add build cache management
- Add git clone and update for builds
- Add Docker image building
- Add source-to-image builder
- Add legacy HTTP server
- Add unified API server with MCP and REST
- Add embedded web assets for production
- Add up command for topology deployment
- Add down command for topology teardown
- Add status command for topology info
- Add HTML entry point
- Add Vite logo asset
- Add React logo asset
- Add global CSS styles
- Add TypeScript type definitions
- Add classname utility
- Add UI constants
- Add API client for backend
- Add topology to React Flow transform
- Add topology state store
- Add UI state store
- Add keyboard shortcuts hook
- Add polling hook for status updates
- Add Badge component
- Add Button component
- Add IconButton component
- Add StatusDot component
- Add ControlBar component
- Add LogViewer component
- Add ToolList component
- Add Header layout component
- Add Sidebar layout component
- Add StatusBar layout component
- Add React Flow node type registry
- Add CustomNode for agent visualization
- Add GatewayNode for gateway visualization
- Add React Flow Canvas component
- Add React app entry point
- Add main App component
- Add bottom panel state management to UI store
- Add collapsible bottom panel for log viewing
- Add Cmd/Ctrl+J shortcut for bottom panel toggle
- Integrate bottom panel into main layout
- Add Agent struct to topology configuration
- Add validation rules for agent configuration
- Add env expansion and path resolution for agents
- Add agent label constant and helper function
- Add agent container lifecycle management
- Add agent status to API response
- Add agent support to deploy command
- Add MCP_ENDPOINT injection for agent containers
- Add agent access control to MCP gateway
- Add X-Agent-Name header support for tool access control
- Register agents with gateway for access control
- Add runtime and prompt fields for headless agents
- Add validation for headless agent schema
- Add AgentStatus and AgentNodeData types
- Add tertiary color palette for agent nodes
- Add agent nodes and edges to graph transform
- Add agents state to topology store
- Add circular AgentNode component
- Register AgentNode in React Flow node types
- Add agent count to gateway node display
- Add agent color to minimap node display
- Add agent-specific details to sidebar
- Add Command field to Agent config struct
- Pass agent Command to container config
- Add A2A protocol package with types, client, and gateway
- Add A2A configuration types to topology config
- Add validation for A2A config and remote agents
- Integrate A2A gateway into deployment
- Add A2A API endpoints to HTTP server
- Add A2A agent types to web frontend
- Add A2A layout constants
- Add A2A agent node and edge transformation
- Add A2A agent state to topology store
- Add A2AAgentNode component with teal theme
- Register A2AAgentNode in node types
- Add A2A agent edge coloring
- Add A2A agent count to gateway node
- Add A2A agent details to sidebar
- Populate equipped_skills from uses field
- Add cycle detection for agent dependencies
- Add dependency graph with topological sort
- Start agents in dependency order
- Add A2A-to-MCP adapter for agent skills
- Register A2A agent adapters on deploy
- Add dagre layout with LR hierarchy
- Unified agent node with variant styling
- Add logging package with discard handler
- Add structured logging to MCP gateway
- Add structured logging to runtime operations
- Add host.docker.internal mapping to containers
- Configure structured logging in deploy command
- Add tool name delimiter constant to frontend
- Add SSE transport type constant
- Add URL field and IsExternal helper for MCP servers
- Add validation for external MCP servers
- Skip container creation for external MCP servers
- Add SSE transport handling and External field to gateway
- Register external MCP servers and preserve on daemon restart
- Add transport and external fields to API response
- Add SSE transport and external field to frontend types
- Pass external field from API to node data
- Add transport icon and color utility functions
- Add violet styling and External badge for external servers
- Add external server styling to sidebar details
- Add mock MCP server for testing external servers
- Add example topology for external MCP servers
- Add IsLocalProcess helper for config detection
- Add validation for local process MCP servers
- Add ProcessClient for local stdio MCP servers
- Add local process support to MCP gateway
- Add local process fields to MCPServerInfo
- Register local process servers in deploy command
- Add LocalProcess field to API status response
- Add localProcess field to frontend types
- Include localProcess in MCP server node data
- Add local process indicator to MCP server nodes
- Add local process MCP server example
- Add SSH config type for remote MCP servers
- Add SSH config loading and env expansion
- Add SSH MCP server validation rules
- Add SSH transport support in MCP gateway
- Register SSH MCP servers with gateway
- Pass SSH config to runtime during deploy
- Expose SSH host in MCP server status API
- Add SSH fields to MCP server status types
- Add workload type to container status response
- Add --base-port flag for MCP server ports
- Add mock-servers and clean-mock-servers make targets
- Add configurable PORT param to mock-servers target
- Add GoReleaser configuration
- Add version command with ldflags
- Update release workflow for GoReleaser

### Refactoring


- Simplify CustomNode with clean design patterns
- Simplify GatewayNode with clean design patterns
- Integrate bottom panel and remove log viewer overlay
- Rename up command to deploy
- Rename down command to destroy
- Remove old up and down commands
- Register deploy and destroy commands
- Add equipped_skills field to agent config
- Filter A2A adapters from MCP server status
- Unify agent status with A2A info
- Unify AgentStatus and AgentNodeData types
- Remove A2A_AGENT node type constant
- Unify agent nodes with arrowhead edges
- Remove separate a2aAgents state
- Remove A2AAgentNode from registry
- Delete deprecated A2AAgentNode component
- Update minimap colors for unified agents
- Unified agent details in sidebar
- Change tool name delimiter from -- to ::
- Simplify MCP client result unmarshaling
- Simplify stdio client result unmarshaling
- Update parsePrefixedToolName for :: delimiter
- Remove unused LOCAL_PROCESS_STYLES constant
- Define WorkloadRuntime interface for runtime abstraction
- Add Orchestrator for runtime-agnostic workload management
- Add factory functions for runtime instantiation
- Add backward compatibility types and helpers
- Implement DockerRuntime as WorkloadRuntime
- Remove legacy runtime implementation files
- Update deploy command for new runtime API
- Update destroy command for new runtime API
- Update status command for new runtime API
- Update API server for new runtime types
- Update state management for new runtime types
- Enhance health endpoint to verify MCP server initialization
- Add file locking and graceful daemon shutdown
- Replace sleep with health polling on deploy
- Add locking to destroy command
- Remove unused A2A capability fields
- Change default gateway port to 8180
