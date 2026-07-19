package mcpauth

import (
	"fmt"
	"html"
	"net/http"
)

// CallbackPath is the route the broker's redirect URI points at. It is
// mounted on the gateway's existing listener, outside the inbound API auth
// middleware: the callback authenticates via the single-use state
// parameter, and the browser performing the redirect holds no gateway
// bearer token.
const CallbackPath = "/oauth/callback"

// CallbackHandler returns the HTTP handler for the authorization redirect.
// It completes the pending flow and renders a small self-closing page; all
// real error detail stays on the CLI/API waiter side.
func (b *Broker) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()

		if e := q.Get("error"); e != "" {
			// Deliver the AS error (e.g. access_denied) to the waiter so
			// the CLI/UI reports it immediately instead of running out the
			// flow timeout, then tell the user to look back at gridctl.
			if stateToken := q.Get("state"); stateToken != "" {
				b.FailAuthorization(stateToken,
					fmt.Errorf("authorization server returned error: %s (%s)", e, q.Get("error_description")))
			}
			writeCallbackPage(w, http.StatusBadRequest, "Authorization failed",
				"The authorization server reported: "+e+". You can close this window and retry from gridctl.")
			return
		}

		code, stateToken := q.Get("code"), q.Get("state")
		if code == "" || stateToken == "" {
			writeCallbackPage(w, http.StatusBadRequest, "Invalid callback",
				"The redirect is missing required parameters. You can close this window.")
			return
		}

		if err := b.CompleteAuthorization(r.Context(), stateToken, code, q.Get("iss")); err != nil {
			writeCallbackPage(w, http.StatusBadRequest, "Authorization failed",
				"gridctl could not complete the authorization. Check the CLI or UI for details, then retry.")
			return
		}

		writeCallbackPage(w, http.StatusOK, "Authorization complete",
			"You can close this window and return to gridctl.")
	})
}

// writeCallbackPage renders the minimal self-closing result page. Message
// content is escaped; no query parameters are ever echoed back.
func writeCallbackPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	page := `<!DOCTYPE html>
<html>
<head><title>` + html.EscapeString(title) + `</title></head>
<body style="font-family: system-ui, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0;">
<div style="text-align: center;">
<h1 style="font-size: 1.3rem;">` + html.EscapeString(title) + `</h1>
<p>` + html.EscapeString(message) + `</p>
</div>
<script>setTimeout(function(){ window.close(); }, 2000);</script>
</body>
</html>`
	_, _ = w.Write([]byte(page))
}
