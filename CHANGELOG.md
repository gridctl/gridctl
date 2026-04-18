# Changelog

All notable changes to gridctl will be documented in this file.

## [Unreleased]


### Bug Fixes


- Make MCP HTTP/SSE readiness timeout configurable via `ready_timeout` and remove orphan containers on timeout
- Register logical container name as DNS alias for inter-container resolution in Podman and Docker stacks
- Persist tool turns to history and populate FormatSavingsPct
- Persist tool turns to history and capture streaming usage metrics
- Persist intermediate tool turns in handlePlaygroundChat goroutine
- Strip CLAUDECODE env var and fix CLI proxy stream parsing
- Reorder YAML highlight regexes to prevent HTML class name corruption
- Dedent standalone agent/mcp-server/resource YAML to valid root level
- Add POST /api/stack/append endpoint
- Add appendToStack API client function
- Wire deploy button onClick in ReviewStep
- Pass onDeploy callback from wizard to ReviewStep
- Show pending agents on canvas after wizard deploy
- Refresh graph when active skill count changes
- Remove playground from BottomPanelTab type
- Remove Playground tab from bottom panel
- Remove playground keyboard shortcut and App binding
- Cast tab comparison to avoid orphaned type error
- Exclude skill and skill-group from log agent name lookup
- Import IFuseOptions directly to avoid namespace error
- Remove showSkillsOnCanvas toggle from UIStore
- Remove showSkillsOnCanvas gate from createAllNodes
- Remove showSkillsOnCanvas gate from createAllEdges
- Remove showSkillsOnCanvas from graph transform pipeline
- Stop passing showSkillsOnCanvas in stack store refresh
- Remove skills canvas toggle button and handler
- Add ExternalLink hint on GatewayNode skills row hover
- Remove docker pkg/archive dependency broken in v28
- Update deprecated docker API types for v28 compatibility
- Remove deprecated NetworkSettingsBase from test composite literals
- Update daemon child spawn to use apply command
- Update deploy references to apply in error messages
- Add missing agent logs endpoint
- Apply template selection to mcp-server form data
- Suppress gosec G101 false positives on url path and model name
- Report PASSING (N skipped) when criteria are partially skipped
- Use WithEndpointURL for scheme-aware OTLP TLS
- Flush tracing spans on gateway shutdown
- Stabilize useDriftedServers selector reference
- Correct template expression syntax in multi-step DAG test
- Resolve parent relative paths before merging into child
- Correct gridctl apply command in tracing example comment
- Populate InitializeResult instructions for gateway discoverability
- Set Title to prefixed name and strengthen Description in AggregatedTools
- Bump MCPProtocolVersion to 2025-11-25

### Features


- Graduate Podman runtime from experimental to stable; `gridctl info` and `--runtime` help no longer show experimental status
- Add netavark and aardvark-dns detection for rootless Podman; `gridctl apply` warns with install instructions when either is missing
- Add Podman 4.0+ version gate warning for rootless multi-container networking
- Add `gridctl info` network stack display for rootless Podman (shows netavark + aardvark-dns status)
- Add multi-container networking integration test proving rootless Podman inter-container DNS works on a shared named network
- Add TestFlightSession, LLMClient interface, and SessionRegistry
- Add Anthropic LLM client with streaming agentic loop
- Add OpenAI-compatible LLM client for Ollama and hosted APIs
- Register playground routes and session registry on Server
- Add playground HTTP handlers for auth, chat, stream, and session
- Add ToolCallBlock and multi-turn tool history persistence
- Implement playground auth detection endpoint
- Add playground API client functions
- Add usePlaygroundStore for session and message state
- Add PlaygroundTab with auth banner and SSE chat
- Register playground as bottom panel tab
- Add keyboard shortcuts for spec, traces, and playground tabs
- Add SSE streaming state to usePlaygroundStore
- Wire SSE events and render streaming tokens in PlaygroundTab
- Add ReasoningWaterfall component with expand/collapse
- Integrate ReasoningWaterfall into PlaygroundTab
- Add isThinking and isProcessing to node data types
- Animate edges and nodes during playground tool calls
- Add thinking ring to AgentNode during test flight
- Add processing badge to CustomNode during active tool calls
- Add showAgentBuilderMode state to useUIStore
- Add PATCH /api/playground/agent endpoint for agent config updates
- Add AgentBuilderInspector with Config/Tools/Preview tabs
- Add agent builder mode toggle and inspector to Canvas
- Add GeminiClient LLM implementation for Gemini API
- Add Gemini routing and equippedSkills to playground API
- Add selectedModel and ollamaEndpoint to usePlaygroundStore
- Add draftEquippedSkills state for A2A edge wiring
- Add multi-provider model selector to PlaygroundTab
- Add A2A edge wiring handler in Agent Builder Mode
- Show A2A peers and equipped_skills in AgentBuilderInspector
- Add CLIProxyClient for Path B claude CLI subprocess
- Add gatewayAddr field and SetGatewayAddr to API server
- Set gateway addr on API server during build
- Wire CLI proxy auth mode and fix format savings metrics
- Add CLI proxy option and example prompts to PlaygroundTab
- Add acceptance criteria and test result types
- Add acceptance criteria runner to registry executor
- Add in-memory test result persistence to registry store
- Add TestSkill method to registry server
- Add POST /api/registry/skills/{name}/test endpoint
- Add gridctl activate command with criteria enforcement
- Add gridctl test command for acceptance criteria runner
- Add runSkillTest and getSkillTestResult API functions
- Add acceptance criteria editor to skill form
- Add test status badge to skill items in registry sidebar
- Add skill node type constant
- Add SkillNodeData type and SkillTestStatus
- Add SKILLS zone and gateway-to-skill edge type
- Add skill node dimensions to layout utils
- Add skills zone to butterfly layout engine
- Add createSkillNodes and wire skills into createAllNodes
- Add createGatewayToSkillEdges and wire into createAllEdges
- Thread skills through graph transform pipeline
- Add SkillNode canvas component with state and test badges
- Register SkillNode in React Flow node type map
- Pass active skills to graph transform on refresh
- Expose AgentSkill.Dir field in API responses
- Add dir field to AgentSkill and SkillGroupNodeData type
- Add SKILL_GROUP to NODE_TYPES constant
- Add gateway-to-skill-group edge relation type
- Replace createSkillNodes with createSkillGroupNodes
- Replace skill edges with group-based edge creator
- Map skill-group type to SKILLS zone in butterfly layout
- Add SkillGroupNode component for directory-based grouping
- Register skillGroup node type
- Open registry sidebar when skill-group node is clicked
- Add showSkillsOnCanvas toggle to useUIStore
- Gate skill group nodes behind showSkillsOnCanvas flag
- Gate skill group edges behind showSkillsOnCanvas flag
- Thread showSkillsOnCanvas through graph transform pipeline
- Pass showSkillsOnCanvas to graph refresh in useStackStore
- Add BookOpen toggle button for skills canvas visibility
- Make GatewayNode skills row clickable to open registry
- Add useFuzzySearch hook with fuse.js
- Add SkillCard component with all variants
- Upgrade DetachedRegistryPage to card grid dashboard
- Add mcp-basic.yaml as canonical getting-started example
- Add apply command (rename from deploy)
- Remove deploy command
- Register apply command in root
- Add --auto-approve flag to plan command
- Add tokenizer field to GatewayConfig
- Replace heuristic counter with cl100k embedded tokenizer
- Wire buildTokenCounter from gateway config
- Expose active tokenizer in /api/status response
- Add tokenizer badge to StatusBar
- Implement APICounter with Anthropic count_tokens endpoint
- Wire api tokenizer mode in buildTokenCounter
- Add pinStatus and pinDriftCount to MCPServerNodeData
- Add ServerPins types and pins API functions
- Add usePinsStore with drift server selector
- Annotate MCP nodes with pin state in refreshNodesAndEdges
- Poll GET /api/pins and refresh nodes on each cycle
- Extend Toast with warning type and action prop
- Add pins tab to BottomPanelTab type
- Add PinDriftBadge status bar component
- Add PinsPanel bottom panel tab
- Wire PinDriftBadge into StatusBar
- Register pins tab in BottomPanel
- Add drift and blocked indicators to CustomNode
- Fire drift toast on first schema drift detection
- Add exit code 3 for all-skipped test result
- Add parseable-criteria gate to activate command
- Mirror parseable-criteria gate in API activate endpoint
- Add --dry-run flag to gridctl test command
- Add Extends field to Stack struct for composition
- Implement stack composition via extends field
- Add KnownHostsFile and JumpHost fields to SSHConfig
- Expand and resolve new SSH fields in loader
- Validate knownHostsFile and jumpHost SSH fields
- Add knownHostsFile and jumpHost support to buildSSHCommand
- Pass SSHKnownHostsFile and SSHJumpHost through registration paths
- Add crypto.randomUUID() to code mode sandbox
- Add setTimeout, clearTimeout, and sleep() to sandbox
- Add fetch config field and Promise unwrapping to sandbox
- Add sandboxed fetch with SSRF mitigations

### Refactoring


- Remove pkg/runtime/agent package
- Remove pkg/a2a package
- Remove pkg/adapter package
- Remove agent types from config
- Remove agent validation from config
- Remove agent health checks from config
- Remove agent loading from config loader
- Remove agent plan diff logic
- Remove agent scoping from mcp gateway
- Remove agent references from mcp handler
- Remove agent streaming from mcp
- Remove agent diff from reload
- Remove agent startup from reload handler
- Remove agent orchestration from runtime
- Remove agent wiring from controller
- Remove agent registration from gateway builder
- Remove agent api handlers and routes
- Remove playground feature
- Remove agent handling from stack api
- Remove agent sanitization from export
- Remove agent types from frontend
- Remove agent constants
- Remove agent node creation from graph
- Remove agent edge creation from graph
- Remove agent transform from graph
- Remove agent zone from butterfly layout
- Remove agent graph utilities
- Remove agent yaml generation
- Remove agent state from stack store
- Remove agent form data from wizard store
- Remove agent ui state
- Delete agent graph components
- Remove agent node type from canvas registry
- Remove agent builder from canvas
- Remove agent stats from gateway node
- Remove agent wiring from overlay
- Delete agent wizard form
- Remove agent option from creation wizard
- Remove agent references from stack form
- Remove agent panel from sidebar
- Remove agent controls from control bar
- Remove agent references from spec overlay
- Remove agent path highlight logic
- Remove agent panel from detached sidebar
- Remove agent references from detached logs
- Upgrade RegistrySidebar search to fuse.js
- Remove mock-based test file from integration package
- Register stack routes with Go 1.22 method+path patterns
- Remove handleStack dispatcher, use direct route handlers
- Register traces routes with Go 1.22 method+path patterns
- Replace manual path parsing with PathValue in handleTraces
- Register wizard routes with Go 1.22 method+path patterns
- Remove handleWizard dispatcher, use PathValue for draft ID
- Register pins routes with Go 1.22 method+path patterns
- Remove handlePins dispatcher, use PathValue for server name
- Remove handleAgentAction and handleMCPServerAction dispatchers, use PathValue
- Register skills routes with Go 1.22 method+path patterns
- Remove handleSkills dispatcher, use PathValue for source name
- Register vault routes with Go 1.22 method+path patterns
- Remove handleVault dispatcher, use PathValue for key and name
- Register registry routes with Go 1.22 method+path patterns
- Remove handleRegistry dispatcher, use PathValue for skill routes## [0.1.0-beta.4] - 2026-03-26


### Bug Fixes


- Suppress errcheck for cleanup chmod in test
- Resolve golangci-lint issues in mcp tests
- Resolve golangci-lint errors in tracing package
- Add response DTOs to align traces API with frontend contract
- Defer window.open to next frame to prevent popout flash
- Eager-import detached pages to eliminate Suspense flash on popout
- Extract server.name from child spans when root span lacks it
- Improve server.name fallback scan in buffer
- Propagate server.name to root span for trace filtering
- Populate server dropdown from deployed MCP servers
- Populate server dropdown in detached traces window
- Improve metrics graph axis label contrast
- Use state.PinsPath, normalize null/empty schemas to {}

### Features


- Add MaxToolResultBytes to GatewayConfig
- Validate MaxToolResultBytes in gateway config
- Add TruncateResult with UTF-8 safe truncation
- Wire tool result truncation into gateway
- Configure max tool result bytes from stack.yaml
- Add LoggingConfig to stack config
- Add LogFile field to controller Config
- Add file handler and multi-handler for log output
- Add --log-file flag to deploy command
- Pass --log-file through to daemon child process
- Wire log file output into gateway logging init
- Implement MCP Streamable HTTP transport
- Wire StreamableHTTPServer and add /api/sessions endpoint
- Add TracingConfig to GatewayConfig
- Add tracing package with OTel provider and buffer
- Add _meta traceparent injection for stdio transports
- Extract W3C trace context in MCP HTTP handler
- Create OTel child spans in gateway tool call path
- Inject traceparent header into outgoing HTTP requests
- Inject _meta traceparent into stdio/process transports
- Add /api/traces REST endpoints
- Initialize tracing provider in gateway builder
- Add root spans for all MCP methods and fix semantic conventions
- Add mcp.format_conversion child span
- Add trace activity summary to gridctl status
- Add gridctl traces command with table and waterfall output
- Add traces API types and fetch functions
- Extend UI store with traces detached and latency heat state
- Add useTracesStore with polling, filters, and trace detail
- Add SpanDetail panel with timing, attributes, and events
- Add TraceWaterfall with server colors, error and p95 highlighting
- Add TracesTab with filterable table and inline waterfall
- Add Traces tab to bottom panel
- Add DetachedTracesPage for pop-out traces window
- Add useLatencyHeat hook for canvas edge latency overlay
- Add traces to window manager and broadcast channel
- Add /traces route for detached traces window
- Add font size zoom controls to traces tab
- Add font size zoom controls to detached traces window
- Add AcceptanceCriteria field to AgentSkill
- Serialize AcceptanceCriteria in RenderSkillMD
- Warn on executable skills with no acceptance criteria
- Add skill validate command and acceptance criteria display
- Add pins package data types and constants
- Add PinStore with TOFU hashing and atomic persistence
- Add PinsDir and PinsPath to state package
- Add GatewaySecurityConfig and SchemaPinningConfig to gateway
- Add SchemaDrift and SchemaVerifier types for TOFU pinning
- Add GatewayAdapter to bridge PinStore to SchemaVerifier
- Wire SchemaVerifier into Gateway with drift policy enforcement
- Propagate PinSchemas field through MCPServerConfig builders
- Add PinResetter interface for optional pin store clearing
- Add ResetServerPins to Gateway for hot reload pin invalidation
- Implement PinResetter on GatewayAdapter
- Reset schema pins on server removal and config change during reload
- Add pins CLI subcommands for schema pin management
- Add pins CRUD API handler
- Wire pin store and register pins endpoints
- Inject pin store into API server via gateway builder
- Add pin status column to server output table
- Load and display pin status in status command
- Add commandPaletteOpen state to useUIStore
- Add Cmd+K binding to useKeyboardShortcuts
- Add PaletteCommand type definitions
- Add useCommandRegistry hook with frecency scoring
- Add showVault state to useUIStore for palette access
- Add CommandPalette component using cmdk
- Add useGlobalCommands hook for static and dynamic commands
- Add command palette trigger button to Header
- Wire CommandPalette and CommandRegistryProvider into App
- Add unavailable flag to PaletteCommand type
- Add unavailable command state with toast error and enhanced empty state

### Refactoring


- Return session from HandleInitialize
- Update handler to consume new HandleInitialize signature
- Strip sse.go to legacy negotiation redirect only

### Revert


- Restore synchronous window.open for trusted gesture context## [0.1.0-beta.3] - 2026-03-18


### Bug Fixes


- Add missing MarkerType to xyflow mock in CustomNode tests
- Resolve strict type errors in test mocks and form
- Remove unused registryDir method
- Update registry panel test for renamed button
- Remove unused variable and import to fix build
- Remove redundant newline in export JSON output
- Remove unused useCallback import from SpecModeOverlay
- Remove redundant newline in skill try output
- Add Replace flag to controller for plan apply on running stacks
- Use Replace flag in plan apply instead of manual teardown
- Add appliedSpec baseline to decouple polling from diff
- Compare against appliedSpec in reload config flow
- Render diff modal via portal with full-viewport layout
- Skip auth for static web UI paths
- Render creation wizard via portal to prevent viewport clipping
- Skip template step for secret type in creation wizard
- Pass handleTypeSelect to TypePicker to skip template step
- Open vault panel directly when selecting secret type
- Render vault panel via portal to prevent clipping
- Use absolute base path for sub-route asset resolution
- Add inline dark background to prevent white flash
- Set hardcoded background on html/body/root elements
- Skip sidebar transition on detachment
- Skip bottom panel transition on detachment

### Features


- Add token counting interface with heuristic implementation
- Add metrics accumulator with ring buffer
- Add metrics observer bridging gateway to accumulator
- Add ToolCallObserver interface for metrics collection
- Hook token counting observer into HandleToolsCall
- Add token_usage to status API and metrics endpoints
- Wire token counter and metrics accumulator in server startup
- Add token usage types and extend GatewayStatus
- Extend store with token usage from status response
- Add fetchTokenMetrics and clearTokenMetrics API functions
- Add formatCompactNumber utility
- Add token counter and savings indicator to status bar
- Add recharts dependency for chart visualizations
- Add metrics polling interval constant
- Add bottom panel tab state for logs/metrics switching
- Add Tremor Raw chart components adapted for Obsidian theme
- Extract LogsTab from BottomPanel into standalone component
- Add MetricsTab with KPI cards, area chart, and server table
- Add SparkChart component for inline sparkline visualizations
- Export SparkChart from chart component barrel
- Add per-server token usage section with sparkline and savings
- Integrate token usage section into sidebar for MCP servers
- Add heat map and metrics detached state to UI store
- Extend broadcast channel with metrics window type
- Add metrics window type to window manager
- Add token heat intensity hook for graph nodes
- Add keyboard shortcuts for bottom panel tab switching
- Add token heat overlay glow to MCP server nodes
- Add heat map toggle button to canvas controls
- Add popout button to metrics tab
- Add detached metrics window page
- Register detached metrics page route
- Wire tab switching shortcuts in app component
- Add TOON v3.0 output format converter
- Add CSV output format converter
- Add format dispatcher with json and text support
- Add OutputFormat field to GatewayConfig and MCPServer
- Add output_format validation for gateway and servers
- Add FormatSavingsRecorder interface
- Add gateway format conversion pipeline
- Add RecordFormatSavings to accumulator
- Pass OutputFormat through ServerRegistrar
- Wire format conversion in gateway builder
- Add toon and csv to valid output formats
- Add toon and csv output assembly
- Add OutputFormat to MCPServerStatus
- Pass OutputFormat through API status
- Add outputFormat to TypeScript types
- Pass outputFormat to node data
- Add format badge to server nodes
- Add output format row to sidebar
- Add spec validation with severity levels
- Add spec plan diff engine
- Add gridctl validate command
- Add gridctl plan command
- Register validate and plan commands
- Add stack file setter and spec route registration
- Add stack spec API endpoints
- Wire stack file path to API server
- Add spec visibility TypeScript types
- Add stack spec API client functions
- Add spec tab to bottom panel tab type
- Add spec Zustand store
- Add spec tab with syntax highlighting and validation
- Add spec health badge for status bar
- Add spec diff modal for config reload
- Add spec components barrel export
- Integrate spec tab into bottom panel
- Add spec health badge to status bar
- Wire reload button to spec diff modal
- Add skill source config and semver resolution
- Add origin sidecar for imported skills
- Add skills lock file for version pinning
- Add remote skill clone and discovery
- Add security scanner for imported skills
- Add skill import orchestration
- Add background skill update checker
- Add skill CLI commands for remote import
- Register skill command in root
- Add wizard draft CRUD API endpoints
- Register wizard API routes
- Add form-to-YAML serialization utility
- Add wizard draft API client functions
- Add wizard Zustand store with session persistence
- Add template selection grid component
- Add live YAML preview with validation annotations
- Add Form/YAML expert mode toggle
- Add named draft save/load/delete manager
- Add spec review step with validation gate
- Add creation wizard modal with type picker and split-pane
- Add create resource button to header
- Add secrets popover for inline vault integration
- Add 6-variant dynamic MCP server form
- Wire MCP server form into creation wizard
- Add stack spec composition form with nested sub-forms
- Wire stack form into creation wizard
- Add empty-state canvas CTA for stack creation
- Add skill import and update TypeScript types
- Add skill source API client functions
- Add Dir accessor to registry Store
- Add skill source REST API endpoints
- Register skill source routes in API server
- Add source URL input step for skill import
- Add skill browse and preview step
- Add 4-step skill import wizard
- Integrate skill import wizard into creation flow
- Add import button and update badges to sidebar
- Add agent spec form with container/headless/A2A support
- Add resource spec form with database presets
- Wire agent and resource forms into creation wizard
- Add quick-add links to empty canvas CTA
- Add background skill update check on startup
- Show skill update notice after deploy
- Add drift overlay toggle to UI store
- Add drift overlay component for spec-vs-running state
- Export DriftOverlay from spec barrel
- Integrate drift overlay toggle into canvas controls
- Add bulk update all button to registry sidebar
- Add skill fingerprinting with behavioral change detection
- Add fingerprint field to skill origin tracking
- Add fingerprint field to skill lock entries
- Integrate fingerprint computation into skill import and update
- Add gridctl export command for spec reverse-engineering
- Enhance skill try with countdown display and signal handling
- Add export, secrets-map, and recipes API endpoints
- Add spec mode, wiring mode, and heatmap toggles to UI store
- Add export, secrets-map, and recipes API client functions
- Add canvas spec mode overlay with ghost and warning nodes
- Add secret heatmap overlay with color-coded shared secrets
- Add wiring mode overlay for agent-server connections
- Integrate spec mode, wiring mode, and heatmap into canvas
- Add stack recipe picker with category filtering
- Add transport compatibility advisor for wizard
- Integrate transport advisor into MCP server form
- Add vaultDetached state to UI store
- Add vault to detached window sync type
- Add vault window management with instant detach
- Add vault route with dark suspense fallback
- Rewrite vault panel with search, resize, and popout
- Add detached vault page for pop-out window

### Refactoring


- Convert BottomPanel to tabbed container for logs and metrics
- Fix staticcheck QF1003, QF1012, ST1005, ST1023 issues## [0.1.0-beta.2] - 2026-03-11


### Bug Fixes


- Skip vault ref validation when no vault provided
- Auto-unlock vault with env passphrase on deploy
- Pass vault context through reload handler
- Wire vault store into hot reload handler## [0.1.0-beta.1] - 2026-03-09


### Bug Fixes


- Remove unused type flagged by linter
- Make security scans non-blocking in CI
- Lower controller coverage threshold to 59%
- Resolve TypeScript errors in GatewayPanel test
- Remove unused import in LogViewer test
- Remove unused imports in hooks test

### Features


- Add workflow types to registry package
- Render workflow fields in SKILL.md
- Add workflow DAG builder with cycle detection
- Add workflow validation rules
- Add template engine for skill workflows
- Add ToMCPTool method for executable skills
- Add workflow executor engine
- Integrate executor with registry server
- Wire executor into gateway builder
- Add workflow REST API endpoints
- Add parallel execution, retry, and timeout to workflow executor
- Pass executor options through server constructor
- Add workflow TypeScript types
- Add workflow API functions
- Add workflow text zoom and blink CSS
- Add workflow Zustand store
- Add workflow detached window state
- Add workflow font size zoom hook
- Support workflow window in broadcast channel
- Add workflow window config to manager
- Add StepNode React Flow component
- Add WorkflowGraph DAG visualization
- Add WorkflowInspector step detail panel
- Add WorkflowRunner test panel
- Add WorkflowPanel composition component
- Add workflow tab to SkillEditor
- Add detached workflow pop-out page
- Add workflow route to app router
- Add workflow YAML sync utilities
- Add toolbox palette with drag-and-drop
- Add editable step inspector panel
- Add editable workflow canvas
- Add visual designer composition layer
- Add Code/Visual/Test mode toggle
- Add generalized useTextZoom hook with container props
- Add useContainerWidth hook for responsive layout
- Add workflow keyboard shortcuts hook
- Update workflow pop-out window to 1200x800
- Add execution history and last arguments to workflow store
- Add workflow execution animations and text zoom CSS
- Add execution animations and custom memo comparator to StepNode
- Add edge dash-flow animation for active workflow edges
- Add execution history, error recovery, and dimmed history cards
- Add workflow empty state with template insertion
- Add empty canvas hint for visual designer
- Add responsive layout with container width breakpoints
- Enhance detached workflow with mode toggle and execution sync
- Add workflow badge and quick-open button to skill list
- Add executable badge to registry node in topology graph
- Add VaultDir helper to state package
- Add vault secret type definition
- Add vault store with CRUD and atomic writes
- Add unified expansion with vault resolution
- Add vault value redaction to log handler
- Load vault and wire into deploy pipeline
- Pass vault to gateway for redaction and API
- Add vault store and routes to API server
- Add vault REST API endpoints
- Add vault CLI commands
- Register vault command in CLI
- Add variable set types to vault package
- Add variable set operations to vault store
- Add VaultSetLookup interface for set injection
- Add Secrets config type for variable sets
- Inject variable set secrets into container env
- Wire vault set injection into deploy flow
- Add vault set REST API endpoints
- Add vault sets CLI commands and --set flag
- Add vault API client functions
- Add vault Zustand store
- Add vault management slide-over panel
- Wire settings button to vault panel
- Add encrypted vault types for envelope encryption
- Add XChaCha20-Poly1305 envelope encryption
- Integrate encryption into vault store
- Add vault lock, unlock, and change-passphrase CLI commands
- Add HTTP 423 Locked status constant
- Add vault status, lock, and unlock API endpoints
- Add vault encryption API client functions
- Add lock state management to vault store
- Add vault passphrase unlock prompt component
- Integrate lock/unlock flow into vault panel
- Add skills fields to GatewayNodeData and remove RegistryNodeData
- Pass registry status to gateway node data
- Add skills stat row with monochromatic icon style
- Add embedded prop to RegistrySidebar
- Add GatewaySidebar with embedded registry
- Wire GatewaySidebar into sidebar dispatch
- Add search filtering to registry sidebar
- Add NeedsDocker and IsContainerBased predicates to config
- Defer Ping and EnsureNetwork behind NeedsDocker guard
- Skip Docker status query for non-container stacks
- Graceful destroy when Docker is unavailable
- Show gateway status when Docker is unavailable
- Add compact height constants for all node types
- Add compact option to layout types
- Support compact dimensions in getNodeDimensions
- Pass compact state through butterfly layout engine
- Thread compact option through transform pipeline
- Add compactCards toggle to UI store
- Read compact state when calculating layout
- Add compact rendering to CustomNode
- Add compact rendering to AgentNode
- Add compact rendering to ClientNode
- Add compact cards toggle button to canvas toolbar
- Add runtime detection module for Docker and Podman
- Add NewWithInfo factory for runtime-aware orchestrator creation
- Add runtime-aware host alias and error messages to orchestrator
- Add NewDockerClientWithHost for explicit socket selection
- Add runtime info support to DockerRuntime driver
- Add runtime-aware host alias and SELinux volume labels
- Register runtime-aware orchestrator factory
- Add runtime detection and selection to controller
- Use runtime-aware host alias in reload handler
- Add --runtime persistent flag for runtime selection
- Pass runtime flag from deploy command to controller
- Add gridctl info subcommand for runtime diagnostics
- Print runtime info and rootless warning at deploy startup
- Add individual MCP server restart API and UI

### Refactoring


- Migrate useLogFontSize to delegate to useTextZoom
- Migrate useWorkflowFontSize to delegate to useTextZoom
- Integrate workflow keyboard shortcuts hook
- Use unified expansion in stack loader
- Replace popup window configs with simple tab-based navigation
- Simplify PopoutButton using IconButton component
- Remove redundant tooltip prop from PopoutButton usage
- Remove redundant tooltip prop from sidebar PopoutButton
- Remove redundant tooltip prop from registry PopoutButton
- Remove gateway-to-registry edge
- Remove registry status from edge creation
- Remove gateway-to-registry edge relation type
- Remove registry exports from graph index
- Remove registry zone assignment from layout
- Remove registry dimensions from layout utils
- Remove registry node type and layout constants
- Remove standalone RegistryNode component
- Remove registry from node type registry
- Rename NeedsDocker to NeedsContainerRuntime
- Replace Docker-specific strings with runtime-agnostic text
- Use runtime-agnostic error messages in destroy
- Use runtime-agnostic error message in status## [0.1.0-alpha.11] - 2026-02-27


### Bug Fixes


- Update stale unlink command help text
- Reject HTML responses and warn on OpenAPI 3.1 compat errors
- Check w.Write return values in tests

### Features


- Add OpenCode provisioner for link/unlink
- Register OpenCode in provisioner registry
- Add OpenCode case to simulateLink
- Add code_mode fields to GatewayConfig
- Add code_mode validation rules
- Add esbuild transpiler for code mode
- Add tool search index for code mode
- Add goja sandbox with tool bindings
- Add search and execute meta-tool defs
- Add code mode orchestrator
- Integrate code mode into gateway
- Add CodeMode to controller config
- Wire code mode config to gateway
- Add --code-mode flag to deploy command
- Add code_mode to /api/status response
- Show code mode in gridctl status
- Add Code Mode column to gateway table
- Add code_mode to frontend types
- Extract codeMode in stack store
- Pass codeMode through graph transform
- Pass codeMode to gateway node data
- Add Code Mode badge to gateway node
- Add Code Mode indicator to status bar## [0.1.0-alpha.10] - 2026-02-23


### Bug Fixes


- Use streamable HTTP endpoint for Claude Desktop bridge
- Use streamable HTTP endpoint for Cline bridge## [0.1.0-alpha.9] - 2026-02-19


### Bug Fixes


- Remove unused toolNames helper function
- Update CORS methods and registry comment
- Check return value of w.Write for errcheck
- Handle legacy prompt type in detached editor
- Support recursive skill discovery in nested directories
- Sort skills list for deterministic API responses
- Sort router clients and tools by name
- Sort MCP server statuses by name
- Sort A2A agent lists for stable ordering
- Sort unified agent statuses by name
- Use dedicated registry window for popout
- Add zoom controls and scalable text to sidebar

### Features


- Replace registry types with AgentSkill for agentskills.io spec
- Add skill validator per agentskills.io spec
- Add SKILL.md frontmatter parser and renderer
- Replace skill editor with markdown split-pane layout
- Add file tree browser for skill directories
- Integrate file tree into skill editor
- Improve skills editor UX with resizable panes and larger inputs
- Enlarge detached editor window for better editing
- Add Dir field to AgentSkill for nested path tracking
- Add registryDetached state to UI store
- Add registry type to broadcast channel
- Add registry window management support
- Add dedicated detached registry page
- Add detached registry route

### Refactoring


- Migrate store to directory-based SKILL.md layout
- Update registry server for AgentSkill types
- Remove step-based executor for markdown skills
- Update API endpoints for skills-only registry
- Directory-based skill storage with file management
- Serve agent skills as prompts instead of tools
- Remove ToolCaller from registry server constructor
- Update resource URI scheme to skills://registry/
- Remove executor placeholder file
- Add file management and validation endpoints
- Replace prompt/skill types with AgentSkill model
- Update API client for agent skills registry
- Simplify registry store to skills-only
- Remove prompt fetching from polling hook
- Update registry node to skills-only counts
- Update registry edge condition for skills-only
- Display skills-only counts in registry node
- Remove obsolete prompt editor component
- Remove obsolete skill test runner component
- Rewrite skill editor for AgentSkill model
- Simplify registry sidebar to skills-only
- Update detached editor for skills-only
- Replace sidebar tabs with single skills list view
- Add agent skills sublabel to registry node
- Replace chunk size suppression with vendor splitting
- Lazy-load detached page routes## [0.1.0-alpha.8] - 2026-02-16


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
