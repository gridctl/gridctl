package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	openPort  int
	openStack string
	openPath  string
	openPrint bool
	openJSON  bool
)

// browserOpener launches the OS default browser; swapped in tests.
var browserOpener = openInBrowser

var openCmd = &cobra.Command{
	Use:     "open",
	Aliases: []string{"ui"},
	Short:   "Open the gridctl web UI in a browser",
	Long: `Opens the embedded web UI for a running gateway in the default browser.

The port comes from the first running stack's state; use --stack to pick a
specific stack or --port to override. When no gateway is running the URL is
still printed (default port 8180) with a warning.

Related: 'gridctl status' lists every running gateway and its port.`,
	Example: `  gridctl open                 Open the web UI
  gridctl ui                   Same, via the alias
  gridctl open --print         Print the URL without opening a browser
  gridctl open --stack demo    Open a specific stack's UI`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOpen(os.Stdout, os.Stderr)
	},
}

func init() {
	openCmd.Flags().IntVarP(&openPort, "port", "p", 0, "Port to open (default: first running stack, else 8180)")
	openCmd.Flags().StringVarP(&openStack, "stack", "s", "", "Stack whose UI to open when several are running")
	openCmd.Flags().StringVar(&openPath, "path", "/", "URL path to open")
	openCmd.Flags().BoolVar(&openPrint, "print", false, "Print the URL instead of opening a browser")
	openCmd.Flags().BoolVar(&openJSON, "json", false, "Output the URL as JSON")
}

func runOpen(stdout, stderr io.Writer) error {
	url, running, err := resolveOpenURL(openPort, openStack, openPath)
	if err != nil {
		return err
	}

	if !running && !openJSON {
		fmt.Fprintln(stderr, "Warning: no running gateway detected; start one with 'gridctl apply <stack.yaml>' or 'gridctl serve'")
	}

	switch {
	case openJSON:
		return output.EncodeJSON(stdout, map[string]string{"url": url})
	case openPrint:
		fmt.Fprintln(stdout, url)
		return nil
	default:
		if err := browserOpener(url); err != nil {
			return fmt.Errorf("opening browser: %w (URL: %s)", err, url)
		}
		fmt.Fprintf(stdout, "Opening %s\n", url)
		return nil
	}
}

// resolveOpenURL resolves the UI URL and whether a running gateway backs it.
func resolveOpenURL(port int, stackName, path string) (string, bool, error) {
	states, err := state.List()
	if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("open: could not read state: %w", err)
	}

	running := false
	resolved := port
	switch {
	case stackName != "":
		found := false
		for _, s := range states {
			if s.StackName == stackName && state.IsRunning(&s) {
				if resolved == 0 {
					resolved = s.Port
				}
				found, running = true, true
				break
			}
		}
		if !found {
			return "", false, fmt.Errorf("stack %q is not running (try 'gridctl status')", stackName)
		}
	case resolved != 0:
		for _, s := range states {
			if s.Port == resolved && state.IsRunning(&s) {
				running = true
				break
			}
		}
	default:
		for _, s := range states {
			if state.IsRunning(&s) {
				resolved = s.Port
				running = true
				break
			}
		}
		if resolved == 0 {
			resolved = 8180
		}
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("http://localhost:%d%s", resolved, path), running, nil
}

// openInBrowser launches the OS default browser for the URL.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204 -- URL is built locally from a numeric port and path flag
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204 -- see above
	default:
		cmd = exec.Command("xdg-open", url) // #nosec G204 -- see above
	}
	return cmd.Start()
}
