package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// Exit codes follow the pins/optimize convention (Constitution X).
const (
	authExitOK             = 0
	authExitNeedsAuth      = 1
	authExitInfrastructure = 2
)

// authJSONSchemaVersion identifies the shape of the auth status JSON
// document. Evolution within a version is append-only.
const authJSONSchemaVersion = 1

var (
	authStack        string
	authLoginBrowser bool
	authLoginManual  bool
	authLoginTimeout time.Duration
	authLoginFormat  string
	authLogoutAll    bool
	authStatusFormat string
	authStatusJSON   *bool
	authStatusPlain  *bool
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage downstream server authorization (OAuth)",
	Long: `Authorize gridctl against OAuth-protected remote MCP servers.

This is DOWNSTREAM authorization: gridctl acting as the OAuth client for
external servers declared with 'auth: {type: oauth}' in stack.yaml. It is
unrelated to the gateway's own inbound API auth (gateway.auth in
stack.yaml). One login serves every upstream client connected to gridctl.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login <server>",
	Short: "Authorize a server in the browser",
	Long: `Start the OAuth authorization flow for a server and wait for completion.

Opens the authorization URL in your default browser and waits for the
provider to redirect back to the running gateway. Over SSH, use
--no-browser to print the URL (forward the gateway port, e.g.
'ssh -L 8180:localhost:8180'), or --manual to paste the final redirect
URL back when the browser cannot reach the daemon at all.`,
	Example: `  gridctl auth login notion
  gridctl auth login notion --no-browser
  gridctl auth login notion --manual
  gridctl auth login notion --timeout 10m`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthLogin(os.Stdout, os.Stderr, args[0])
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout [server]",
	Short: "Revoke and delete a server's authorization",
	Long: `Delete the stored tokens for a server, attempting best-effort
revocation with the authorization server first. Use --all to log out of
every OAuth-configured server.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if authLogoutAll {
			return runAuthLogoutAll(os.Stdout)
		}
		if len(args) != 1 {
			return fmt.Errorf("specify a server or use --all")
		}
		return runAuthAction(os.Stdout, args[0], "logout", "Logged out of %s\n")
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status [server]",
	Short: "Show authorization status for OAuth servers",
	Long: `Show downstream authorization status for OAuth-configured servers.

Default output is a table; use '--format json' for machine-readable output.

Exit codes:
  0  every OAuth server is authorized (or none are configured)
  1  at least one server needs authorization
  2  infrastructure error (no running stack, gateway unreachable)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(authStatusFormat, cmd.Flags().Changed("format"), *authStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(authExitInfrastructure)
		}
		if err := resolvePlain(*authStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(authExitInfrastructure)
		}
		server := ""
		if len(args) == 1 {
			server = args[0]
		}
		exit := runAuthStatus(os.Stdout, os.Stderr, server, format)
		if exit != authExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var authResetCmd = &cobra.Command{
	Use:   "reset <server>",
	Short: "Discard a server's tokens and cached client registration",
	Long: `Delete the stored tokens AND the cached dynamic client registration
for a server's authorization server. Use this when a provider-side change
(revoked app, rotated client) leaves login failing; the next login starts
from a clean slate.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthAction(os.Stdout, args[0], "reset", "Reset authorization state for %s\n")
	},
}

func init() {
	authCmd.PersistentFlags().StringVarP(&authStack, "stack", "s", "", "Stack name (default: the single running stack)")

	authLoginCmd.Flags().BoolVar(&authLoginBrowser, "no-browser", false, "Print the authorization URL instead of opening a browser")
	authLoginCmd.Flags().BoolVar(&authLoginManual, "manual", false, "Paste the redirect URL back manually (for SSH sessions)")
	authLoginCmd.Flags().DurationVar(&authLoginTimeout, "timeout", 5*time.Minute, "How long to wait for the browser authorization")
	authLoginCmd.Flags().StringVar(&authLoginFormat, "format", "text", "Output format: text or json")

	authLogoutCmd.Flags().BoolVar(&authLogoutAll, "all", false, "Log out of every OAuth-configured server")

	authStatusCmd.Flags().StringVar(&authStatusFormat, "format", "table", "Output format: table or json")
	authStatusJSON = authStatusCmd.Flags().Bool("json", false, "Shorthand for --format json")
	authStatusPlain = authStatusCmd.Flags().Bool("plain", false, "Plain table output (no styling)")

	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd, authResetCmd)
}

// authServerInfo mirrors pkg/mcpauth.ServerAuthInfo for API decoding.
type authServerInfo struct {
	Server   string     `json:"server"`
	Resource string     `json:"resource"`
	Status   string     `json:"status"`
	Issuer   string     `json:"issuer,omitempty"`
	Scopes   []string   `json:"scopes,omitempty"`
	Expiry   *time.Time `json:"expiry,omitempty"`
}

// authResolveDaemon returns the running daemon state for --stack (or the
// single running stack).
func authResolveDaemon() (*state.DaemonState, error) {
	name := authStack
	if name == "" {
		states, err := state.List()
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("listing stacks: %w", err)
		}
		var running []state.DaemonState
		for _, s := range states {
			if state.IsRunning(&s) {
				running = append(running, s)
			}
		}
		switch len(running) {
		case 0:
			return nil, fmt.Errorf("no running stack found. Deploy a stack first")
		case 1:
			return &running[0], nil
		default:
			names := make([]string, len(running))
			for i, s := range running {
				names[i] = s.StackName
			}
			return nil, fmt.Errorf("multiple stacks running %v. Use --stack to specify one", names)
		}
	}
	st, err := state.Load(name)
	if err != nil {
		return nil, fmt.Errorf("stack '%s' not found", name)
	}
	if !state.IsRunning(st) {
		return nil, fmt.Errorf("stack '%s' is not running. Deploy the stack first", name)
	}
	return st, nil
}

// authAPIClient is the HTTP client for short daemon API calls.
var authAPIClient = &http.Client{Timeout: 15 * time.Second}

func authAPIURL(port int, path string) string {
	return fmt.Sprintf("http://localhost:%d%s", port, path)
}

// fetchAuthServers pulls per-server authorization state from the daemon.
func fetchAuthServers(port int) ([]authServerInfo, error) {
	resp, err := authAPIClient.Get(authAPIURL(port, "/api/auth/servers"))
	if err != nil {
		return nil, fmt.Errorf("gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotImplemented {
		return nil, fmt.Errorf("this gateway has OAuth brokering disabled")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway returned HTTP %d", resp.StatusCode)
	}
	var infos []authServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&infos); err != nil {
		return nil, fmt.Errorf("decoding auth status: %w", err)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Server < infos[j].Server })
	return infos, nil
}

// authPostJSON posts a JSON body to a daemon auth endpoint and decodes the
// response, mapping error payloads onto errors.
func authPostJSON(client *http.Client, apiURL string, body, out any) error {
	payload := []byte("{}")
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("%s", apiErr.Error)
		}
		return fmt.Errorf("gateway returned HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func runAuthLogin(stdout, stderr io.Writer, server string) error {
	st, err := authResolveDaemon()
	if err != nil {
		return err
	}

	var login struct {
		AuthorizeURL string `json:"authorize_url"`
		State        string `json:"state"`
	}
	err = authPostJSON(authAPIClient,
		authAPIURL(st.Port, "/api/servers/"+url.PathEscape(server)+"/auth/login"),
		map[string]int{"timeoutSeconds": int(authLoginTimeout.Seconds())}, &login)
	if err != nil {
		return fmt.Errorf("starting authorization for %s: %w", server, err)
	}

	if authLoginBrowser || authLoginManual {
		fmt.Fprintf(stdout, "Open this URL to authorize %s:\n\n  %s\n\n", server, login.AuthorizeURL)
		if authLoginBrowser && !authLoginManual {
			fmt.Fprintf(stdout, "Waiting for the provider to redirect back to the gateway on port %d.\n", st.Port)
			fmt.Fprintf(stdout, "Over SSH, forward it first: ssh -L %d:localhost:%d <host>\n", st.Port, st.Port)
		}
	} else {
		if err := browserOpener(login.AuthorizeURL); err != nil {
			fmt.Fprintf(stderr, "Could not open a browser (%v). Open this URL manually:\n\n  %s\n\n", err, login.AuthorizeURL)
		} else {
			fmt.Fprintf(stdout, "Opened your browser to authorize %s. Waiting for completion...\n", server)
		}
	}

	if authLoginManual {
		fmt.Fprint(stdout, "After authorizing, paste the full redirect URL here: ")
		reader := bufio.NewReader(os.Stdin)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && line == "" {
			return fmt.Errorf("reading redirect URL: %w", readErr)
		}
		err = authPostJSON(authAPIClient,
			authAPIURL(st.Port, "/api/servers/"+url.PathEscape(server)+"/auth/manual"),
			map[string]string{"redirectUrl": strings.TrimSpace(line)}, nil)
		if err != nil {
			return fmt.Errorf("completing authorization: %w", err)
		}
	} else {
		// The wait endpoint blocks until callback, failure, or timeout; give
		// the HTTP client a slightly longer deadline than the flow itself.
		waitClient := &http.Client{Timeout: authLoginTimeout + 30*time.Second}
		resp, waitErr := waitClient.Get(authAPIURL(st.Port,
			"/api/servers/"+url.PathEscape(server)+"/auth/wait?state="+url.QueryEscape(login.State)))
		if waitErr != nil {
			return fmt.Errorf("waiting for authorization: %w", waitErr)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var apiErr struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != "" {
				return fmt.Errorf("authorization failed: %s", apiErr.Error)
			}
			return fmt.Errorf("authorization failed: gateway returned HTTP %d", resp.StatusCode)
		}
	}

	// Report the resulting grant.
	infos, err := fetchAuthServers(st.Port)
	if err != nil {
		fmt.Fprintf(stdout, "Authorized %s.\n", server)
		return nil
	}
	for _, info := range infos {
		if info.Server != server {
			continue
		}
		if strings.EqualFold(authLoginFormat, "json") {
			return output.EncodeJSON(stdout, info)
		}
		fmt.Fprintf(stdout, "Authorized %s.\n", server)
		if info.Issuer != "" {
			fmt.Fprintf(stdout, "  Issuer: %s\n", info.Issuer)
		}
		if len(info.Scopes) > 0 {
			fmt.Fprintf(stdout, "  Scopes: %s\n", strings.Join(info.Scopes, " "))
		}
		if info.Expiry != nil {
			fmt.Fprintf(stdout, "  Access token expires: %s\n", info.Expiry.Local().Format(time.RFC1123))
		}
		return nil
	}
	fmt.Fprintf(stdout, "Authorized %s.\n", server)
	return nil
}

// runAuthAction posts to a single-server auth endpoint (logout / reset).
func runAuthAction(stdout io.Writer, server, action, successFormat string) error {
	st, err := authResolveDaemon()
	if err != nil {
		return err
	}
	err = authPostJSON(authAPIClient,
		authAPIURL(st.Port, "/api/servers/"+url.PathEscape(server)+"/auth/"+action), nil, nil)
	if err != nil {
		return fmt.Errorf("%s for %s: %w", action, server, err)
	}
	fmt.Fprintf(stdout, successFormat, server)
	return nil
}

func runAuthLogoutAll(stdout io.Writer) error {
	st, err := authResolveDaemon()
	if err != nil {
		return err
	}
	infos, err := fetchAuthServers(st.Port)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Fprintln(stdout, "No OAuth-configured servers.")
		return nil
	}
	for _, info := range infos {
		err := authPostJSON(authAPIClient,
			authAPIURL(st.Port, "/api/servers/"+url.PathEscape(info.Server)+"/auth/logout"), nil, nil)
		if err != nil {
			fmt.Fprintf(stdout, "Logout failed for %s: %v\n", info.Server, err)
			continue
		}
		fmt.Fprintf(stdout, "Logged out of %s\n", info.Server)
	}
	return nil
}

// authStatusDoc is the machine-readable document from 'auth status --format json'.
type authStatusDoc struct {
	SchemaVersion int              `json:"schema_version"`
	Stack         string           `json:"stack"`
	NeedsAuth     bool             `json:"needs_auth"`
	Servers       []authServerInfo `json:"servers"`
}

func runAuthStatus(stdout, stderr io.Writer, server, format string) int {
	st, err := authResolveDaemon()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return authExitInfrastructure
	}
	infos, err := fetchAuthServers(st.Port)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return authExitInfrastructure
	}

	if server != "" {
		filtered := infos[:0]
		for _, info := range infos {
			if info.Server == server {
				filtered = append(filtered, info)
			}
		}
		if len(filtered) == 0 {
			fmt.Fprintf(stderr, "no OAuth configuration for server %q\n", server)
			return authExitInfrastructure
		}
		infos = filtered
	}

	needsAuth := false
	for _, info := range infos {
		if info.Status != "authorized" {
			needsAuth = true
		}
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, authStatusDoc{
			SchemaVersion: authJSONSchemaVersion,
			Stack:         st.StackName,
			NeedsAuth:     needsAuth,
			Servers:       infos,
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return authExitInfrastructure
		}
	} else {
		if len(infos) == 0 {
			fmt.Fprintf(stdout, "No OAuth-configured servers in stack '%s'.\n", st.StackName)
			return authExitOK
		}
		t := output.NewTableWriter(stdout, *authStatusPlain)
		t.AppendHeader(table.Row{"SERVER", "STATUS", "ISSUER", "EXPIRES", "SCOPES"})
		for _, info := range infos {
			statusCell := info.Status
			if info.Status != "authorized" {
				statusCell = fmt.Sprintf("needs auth (run 'gridctl auth login %s')", info.Server)
			}
			expiry := "—"
			if info.Expiry != nil {
				expiry = info.Expiry.Local().Format("2006-01-02 15:04")
			}
			issuer := info.Issuer
			if issuer == "" {
				issuer = "—"
			}
			scopes := strings.Join(info.Scopes, " ")
			if scopes == "" {
				scopes = "—"
			}
			t.AppendRow(table.Row{info.Server, statusCell, issuer, expiry, scopes})
		}
		t.Render()
	}

	if needsAuth {
		return authExitNeedsAuth
	}
	return authExitOK
}

// printAuthHints prints one actionable line per server pending
// authorization after an apply. Best-effort: any API failure keeps apply
// output clean.
func printAuthHints(port int, stdout io.Writer) {
	infos, err := fetchAuthServers(port)
	if err != nil {
		return
	}
	for _, info := range infos {
		if info.Status != "authorized" {
			fmt.Fprintf(stdout, "Server '%s' requires authorization: run 'gridctl auth login %s' or open the web UI (gridctl open)\n",
				info.Server, info.Server)
		}
	}
}
