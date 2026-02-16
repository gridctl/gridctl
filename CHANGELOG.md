# Changelog

All notable changes to gridctl will be documented in this file.

## [Unreleased]


### Bug Fixes


- Use stable ID keys in prompt editor arguments
- Use stable ID keys in skill editor steps and inputs
- Clarify registry node counts with active/total format
- Correct gateway port in multi-agent example docs

### Features


- Add registry types for prompts and skills
- Add file-based registry store with YAML persistence
- Add ToolCaller interface for decoupled tool execution
- Implement ToolCaller on Gateway
- Add registry server implementing AgentClient
- Add registry server field and accessors to API server
- Wire registry server into gateway build pipeline
- Add registry REST API handlers for prompts and skills
- Wire registry routes and enrich status endpoint
- Add MCP prompts and resources protocol types
- Implement PromptProvider interface on registry server
- Add gateway handlers for prompts and resources
- Route prompts and resources methods in HTTP handler
- Route prompts and resources methods in SSE server
- Add registry TypeScript types and node data
- Add registry API client functions
- Add registry Zustand store
- Integrate registry polling into data fetch cycle
- Add registry node type and layout dimensions
- Add gateway-to-registry edge relation type
- Add createRegistryNode with progressive disclosure
- Add gateway-to-registry edge creation
- Pass registry status through graph transform
- Assign registry node to Zone 2 in layout
- Add registry node dimensions to layout utils
- Export registry node and edge functions
- Include registry status in graph refresh
- Trigger graph refresh on registry visibility change
- Add registry graph node component
- Register registry node type in React Flow
- Add registry sidebar with prompts, skills, status tabs
- Route registry node selection to RegistrySidebar
- Add reusable modal component
- Add toast notification system
- Add prompt editor modal
- Add skill editor modal with tool chain builder
- Wire modal editors into registry sidebar
- Add toast container to app layout
- Implement skill CallTool with timeout and state validation
- Add skill execution engine with template resolution
- Add skill test run REST API endpoint
- Add ToolCallResult types for skill test runs
- Add testRegistrySkill API function
- Add skill test runner modal
- Add delete, activate/disable, and test run actions
- Add editorDetached state to UI store
- Add editor type to broadcast channel sync
- Add editor window config and detach handlers
- Add expandable, popout, and flush modes to modal
- Add popout and expand props to prompt editor
- Add popout and expand props to skill editor
- Add detached editor page for popout window
- Register /editor route for detached editor
- Wire popout handlers for prompt and skill editors## [0.1.0-alpha.7] - 2026-02-12


### Bug Fixes


- Add session cap with eviction and count method
- Add periodic session cleanup to MCP gateway
- Add TTL-based cleanup for A2A tasks
- Add periodic A2A task cleanup to gateway
- Wire cleanup goroutines into deploy lifecycle
- Check HandleInitialize error in session count test
- Add context cancellation to stdio transport reader goroutine
- Add context cancellation to process transport reader goroutines
- Add missing docker factory import in integration tests
- Use Ping to verify Docker availability in test
- Remove unused setupMockAgentClientWithCallTool
- Remove empty branch flagged by staticcheck SA9003
- Validate agent identity on SSE tools requests
- Reorder shutdown to broadcast before closing HTTP
- Drain pending requests on all readResponses exit paths
- Drain pending requests on all ProcessClient exit paths
- Data race in ProcessClient between readResponses and Reconnect
- Data race in StdioClient between readResponses and Reconnect
- Add client count display to gateway node
- Use mcpServers wrapper and native SSE for AnythingLLM provisioner
- Upgrade Cursor provisioner to native SSE transport
- Align client nodes with agents in butterfly layout
- Split agent layout dimensions into width and height
- Use separate agent width and height for layout
- Left-align nodes within zones using max width
- Match left-side edges to right-side style
- Only preserve user-dragged node positions
- Use single centered input handle on gateway
- Widen agent node to match client width
- Match client handle size to other nodes
- Wire RedactingHandler into gateway logging chain
- Redact secrets in verbose output and orchestrator logs
- Restrict daemon log file permissions to 0600
- Restrict state file permissions to 0600

### Features


- Add reload package for config hot reload
- Add reload API endpoint and handler support
- Add --watch flag and hot reload integration
- Add reload CLI command
- Add MaxRequestBodySize constant for body limits
- Add GatewayConfig with allowed_origins to stack schema
- Add env var expansion for gateway allowed_origins
- Add body size limit and remove inline CORS from MCP handler
- Add body size limit and remove inline CORS from SSE handler
- Add body size limit and remove inline CORS from A2A handler
- Refactor CORS middleware to accept configurable origins
- Thread allowed origins from stack config to API server
- Add AuthConfig struct to gateway config
- Add validation rules for auth config
- Expand env vars in auth token config
- Add auth middleware for bearer and API key
- Wire auth middleware into HTTP handler
- Add HasAgent method for identity validation
- Validate X-Agent-Name against known agents
- Thread auth config from stack to API server
- Expose session and task counts in status API
- Extend gateway Close to drain client connections
- Add Close method to SSE server
- Add Close method to API server
- Add graceful HTTP shutdown with connection draining
- Add agent identity tracking to SSE sessions
- Include agent identity in MCP_ENDPOINT URL
- Include agent identity in reload MCP_ENDPOINT
- Add SetServerMeta method to gateway
- Add Pingable interface for health checks
- Add Ping method to StdioClient
- Add Ping method to ProcessClient
- Add Ping method to OpenAPIClient
- Add health monitor to gateway
- Expose health status in API responses
- Wire up health monitor in deploy command
- Add health fields to frontend types
- Show health status in graph nodes
- Add Reconnectable interface for MCP clients
- Add reconnection support to StdioClient
- Add reconnection support to ProcessClient
- Trigger reconnection from health monitor
- Add SSE shutdown broadcast notification
- Add shared formatRelativeTime utility
- Add health indicator to MCP server nodes
- Add health details to sidebar status section
- Show unhealthy server count in header
- Show unhealthy count in status bar
- Add openapi fields to MCP server types
- Pass openapi fields through graph node mapping
- Add OpenAPI icon and type badge to graph node
- Add OpenAPI label and spec display to sidebar
- Add session and task count fields to gateway types
- Store session and task counts from status response
- Thread session and task counts through graph transform
- Pass session and task counts to gateway node data
- Display session and A2A task counts in gateway node
- Show session count in status bar
- Add reload API function and result type
- Add reload button with notification to header
- Add auth token management and 401 detection to API layer
- Add auth state store for gateway authentication
- Detect auth errors in polling and pause during auth
- Add auth prompt overlay component
- Integrate auth prompt into app layout
- Differentiate network errors from HTTP errors in polling
- Add SSE shutdown event listener hook
- Add contextual error overlay and shutdown notification
- Add client provisioner registry and interface
- Add platform detection helpers
- Add JSONC read/write with comment detection
- Add config file backup before modification
- Add mcp-remote bridge and npx detection
- Add shared link/unlink logic for MCP clients
- Add Claude Desktop provisioner
- Add Cursor provisioner
- Add Windsurf provisioner
- Add VS Code provisioner
- Add Continue provisioner
- Add Cline provisioner
- Add AnythingLLM provisioner
- Add Roo Code provisioner
- Add link command for LLM client configuration
- Add unlink command to remove client config
- Register link and unlink commands
- Add --flash flag and post-deploy link hint
- Add YAML read/write utilities for provisioner system
- Add httpConfig bridge helper for HTTP-native clients
- Add GatewayHTTPURL, Port field, and register new provisioners
- Extend DryRunDiff for YAML and add new provisioner cases
- Add Claude Code provisioner with custom detection
- Add Gemini CLI provisioner
- Add Zed Editor provisioner
- Add Goose provisioner with YAML config support
- Pass Port in link opts and update supported clients list
- Pass Port in flash link opts for HTTP-native clients
- Add AllClientInfo method for client detection status
- Add /api/clients endpoint for LLM client status
- Wire provisioner registry to API server
- Add ClientStatus and ClientNodeData types
- Add fetchClients API function
- Add client node dimensions and type constant
- Add client zone and edge relation type
- Add client node creation functions
- Add client-to-gateway edge creation
- Add client zone to butterfly layout
- Add client node dimensions to layout utils
- Thread clients through graph transform pipeline
- Re-export client graph functions
- Add ClientNode component for linked LLM clients
- Register client node type
- Add LLM client support to sidebar
- Add clients state to stack store
- Poll /api/clients endpoint
- Add client path highlighting
- Add RedactingHandler for secret redaction in logs

### Refactoring


- Add MCP protocol version and timeout constants
- Use named constants in HTTP MCP client
- Use named constants in stdio MCP client
- Use named constants in process MCP client
- Use named constants in MCP gateway
- Add A2A timeout constant
- Use named timeout constant in A2A client
- Use named constants in A2A adapter
- Use named constant for daemon shutdown grace
- Use named constant for reload HTTP timeout
- Add shared JSON-RPC 2.0 types package
- Re-export JSON-RPC types from shared package in mcp
- Re-export JSON-RPC types from shared package in a2a
- Add Logger field to BuildOptions
- Add LoggerSetter and propagate logger to runtime
- Add logger to DockerRuntime
- Pass logger through builder adapter
- Replace fmt.Printf with slog in git operations
- Replace fmt.Printf with slog in image building
- Initialize and pass logger in builder
- Replace fmt.Printf with slog in image pulling
- Replace fmt.Printf with slog in A2A gateway
- Pass logger to A2A gateway constructor
- Add ClientBase with shared state and accessor methods
- Embed ClientBase in HTTPClient
- Embed ClientBase in StdioClient
- Embed ClientBase in ProcessClient
- Embed ClientBase in OpenAPIClient
- Move label constants from compat to interface
- Use UpResult and Orchestrator directly in CLI
- Remove compat layer after consumer migration
- Remove hand-rolled AgentClient mock
- Add RPCClient base with transporter interface
- Embed RPCClient in HTTP transport client
- Embed RPCClient in stdio transport client
- Embed RPCClient in process transport client
- Remove JSON-RPC type re-exports from mcp package
- Remove JSON-RPC type re-exports from a2a package
- Use jsonrpc types directly in client_base
- Use jsonrpc types directly in mcp handler
- Use jsonrpc types directly in SSE server
- Use jsonrpc types directly in HTTP client
- Use jsonrpc types directly in stdio client
- Use jsonrpc types directly in process client
- Use jsonrpc types directly in a2a handler
- Use DefaultPingTimeout in HTTP client Ping
- Add controller package with Config and StackController
- Add DaemonManager for fork and readiness
- Add ServerRegistrar for MCP server registration
- Add GatewayBuilder for gateway lifecycle
- Slim deploy.go to thin CLI layer over controller
- Remove AnythingLLM special case from simulateLink## [0.1.0-alpha.6] - 2026-02-04


### Bug Fixes


- Prevent selection glow bleedthrough on agent badges
- Add null safety for nodes and edges arrays
- Add null safety for mcpServers array
- Add null safety for mcpServers and resources arrays
- Add null safety for logs array
- Add null safety for tools and whitelist arrays
- Add null safety for graph node creation
- Scale log grid columns with font size and add text wrapping

### Features


- Add kin-openapi dependency for OpenAPI parsing
- Add OpenAPI config types for MCP server definition
- Support env var expansion and path resolution for OpenAPI specs
- Add validation rules for OpenAPI MCP server configuration
- Register OpenAPI clients in MCP gateway
- Implement OpenAPI client for MCP tool transformation
- Handle OpenAPI servers in orchestrator
- Add OpenAPI fields to runtime compatibility types
- Handle OpenAPI transport in deploy command
- Add POSIX-style environment variable expansion for OpenAPI specs
- Add NoExpand config option to OpenAPIClientConfig
- Apply env var expansion when loading local OpenAPI specs
- Add --no-expand flag to disable env var expansion in OpenAPI specs
- Add ResizeHandle component for draggable panel resizing
- Implement CSS Grid layout with resizable panels
- Add BroadcastChannel hook for cross-window sync
- Add window manager hook for detached windows
- Add PopoutButton component for panel headers
- Add detached window state tracking to UIStore
- Add detached logs page with node selector
- Add detached sidebar page with node selector
- Add React Router with detached panel routes
- Add popout button to Sidebar header
- Add popout button to BottomPanel header
- Add in-memory circular log buffer for API
- Add structured slog handler with buffering
- Add /api/logs endpoint for structured gateway logs
- Integrate structured logging with buffer handler
- Add fetchGatewayLogs API function
- Add structured log viewer with filtering
- Add detached logs and sidebar pages
- Add shared log types and parsing utilities
- Add shared LogLine component
- Add shared LevelFilter component
- Add useLogFontSize hook with persistence
- Add ZoomControls component
- Add barrel export for log components
- Add logger support to HTTP MCP client
- Add logger support to stdio MCP client
- Add logger support to process MCP client
- Add logger support to OpenAPI MCP client
- Inject loggers into clients and log tool calls
- Parse Docker timestamps and slog text format in log viewer
- Expand env vars in command, url, and a2a-agent fields
- Capture process stderr and log at warn level
- Add init timing, readiness, and access denial logging
- Share log buffer with orchestrator in foreground mode
- Add Chrome DevTools MCP platform example
- Add Context7 MCP platform example

### Refactoring


- Simplify UI store for panel state management
- Simplify Sidebar to fill parent container
- Simplify BottomPanel to fill grid cell
- Use shared log components and add zoom controls
- Use shared log components and add zoom controls## [0.1.0-alpha.5] - 2026-01-29


### Bug Fixes


- Correct GitHub admonition syntax in README

### Features


- Add Butterfly layout engine for hub-and-spoke visualization
- Add path highlighting hook for agent selection
- Integrate path highlighting into Canvas component

### Refactoring


- Add graph layout type definitions
- Add graph utility functions
- Add Dagre layout engine implementation
- Extract node factory functions to graph module
- Extract edge creation with relation metadata
- Add graph transformation orchestration
- Add graph module public exports
- Extract tool parsing utilities
- Simplify transform.ts to re-export graph module
- Remove legacy layout module## [0.1.0-alpha.4] - 2026-01-28


### Refactoring


- Rename Topology struct to Stack in config types
- Rename LoadTopology to LoadStack
- Update validate to use Stack terminology
- Rename TopologyName/TopologyFile to StackName/StackFile
- Update runtime interface for Stack terminology
- Update orchestrator for Stack terminology
- Update runtime compat for Stack terminology
- Rename LabelTopology to LabelStack
- Update container for Stack terminology
- Update docker driver for Stack terminology
- Update docker network for Stack terminology
- Update a2a client comment for Stack
- Rename topology parameter to stack in builder
- Rename topologyName to stackName in API
- Update deploy command for Stack terminology
- Update destroy command for Stack terminology
- Rename --topology flag to --stack in status
- Update root help text for Stack terminology
- Rename useTopologyStore to useStackStore
- Update App.tsx for useStackStore
- Update Canvas for useStackStore
- Update Header for useStackStore
- Update Sidebar for useStackStore
- Update StatusBar for useStackStore
- Update BottomPanel for useStackStore
- Update ToolList for useStackStore
- Update usePolling for useStackStore## [0.1.0-alpha.3] - 2026-01-27


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
