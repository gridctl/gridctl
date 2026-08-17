package git

import (
	"net/url"
	"strings"
)

// HTTPSEquivalent rewrites an SSH git URL into the HTTPS URL for the same
// repository, reporting false when the input is not SSH or is too malformed to
// convert. Both SCP syntax (git@host:owner/repo.git) and ssh:// URLs convert.
//
// This lives here rather than in a caller so the CLI, the REST layer, and the
// web UI all offer the same rewrite. A non-standard SSH port is dropped: it
// says nothing about where HTTPS is served.
func HTTPSEquivalent(raw string) (string, bool) {
	if DetectProtocol(raw) != ProtocolSSH {
		return "", false
	}

	var host, path string
	if strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// SCP syntax; DetectProtocol already matched [user@]host: via scpSyntax.
		at := strings.Index(raw, "@")
		rest := raw[at+1:]
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return "", false
		}
		host, path = rest[:colon], rest[colon+1:]
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return "", false
	}
	return "https://" + host + "/" + path, true
}
