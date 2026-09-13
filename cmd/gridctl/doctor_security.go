package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/secreport"
)

var (
	doctorSecurity   bool
	doctorSource     string
	securityHTTPDoer secreport.HTTPDoer
)

func validateDoctorSecurityFlags() error {
	if doctorSource != "" && !doctorSecurity {
		return fmt.Errorf("doctor: --source is only valid with --security")
	}
	if doctorSecurity && doctorSource == "" {
		return secreport.ErrSourceRequired
	}
	return nil
}

func runSecurityDoctor(ctx context.Context) (*secreport.Report, error) {
	ref, err := secreport.ParseSource(doctorSource)
	if err != nil {
		return nil, err
	}
	var doer secreport.HTTPDoer
	if ref.Kind == secreport.SourceGateway {
		doer = gatewaySecurityDoer(ref.Value)
	}
	return secreport.Load(ctx, ref, doer)
}

func gatewaySecurityDoer(base string) secreport.HTTPDoer {
	if securityHTTPDoer != nil {
		return securityHTTPDoer
	}
	client := &http.Client{CheckRedirect: secreport.RedirectCheck, Timeout: 15 * time.Second}
	if !localControlPlaneOrigin(base) {
		return unauthenticatedDoer{client: client}
	}
	api := newDaemonAPIForBaseURL(base, 15*time.Second)
	api.client = client
	return authorizedDoer{api: api}
}

func localControlPlaneOrigin(base string) bool {
	u, err := neturl.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "localhost.", "127.0.0.1", "::1", "0:0:0:0:0:0:0:1":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type unauthenticatedDoer struct{ client *http.Client }

func (d unauthenticatedDoer) Do(req *http.Request) (*http.Response, error) {
	return d.client.Do(req)
}

type authorizedDoer struct{ api *daemonAPI }

func (d authorizedDoer) Do(req *http.Request) (*http.Response, error) {
	d.api.authorize(req)
	return d.api.client.Do(req)
}

func renderSecurityReport(w io.Writer, report *secreport.Report, asJSON, quiet bool) int {
	if report == nil {
		return doctorExitFailed
	}
	if asJSON {
		if err := secreport.WriteJSON(w, report); err != nil {
			fmt.Fprintln(os.Stderr, "doctor: encoding report failed")
			return doctorExitFailed
		}
		return secreport.ExitCode(report)
	}
	if err := secreport.WriteText(w, report, quiet); err != nil {
		fmt.Fprintln(os.Stderr, "doctor: writing report failed")
		return doctorExitFailed
	}
	return secreport.ExitCode(report)
}

func securityDoctorMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, secreport.ErrSourceRequired):
		return "doctor: --security requires --source file:<path>, snapshot:<path>, or gateway:<base-url>"
	case errors.Is(err, secreport.ErrSourceInvalid):
		return "doctor: invalid --source; use file:<path>, snapshot:<path>, or gateway:<base-url>"
	case errors.Is(err, secreport.ErrSourceUnsupported):
		return "doctor: unsupported --source kind"
	case errors.Is(err, secreport.ErrSourceUnreadable):
		return "doctor: security source is unreadable or invalid"
	case errors.Is(err, secreport.ErrSourceInvalidFmt):
		return "doctor: unsupported or invalid security snapshot"
	case errors.Is(err, secreport.ErrCredentialURL):
		return "doctor: credential-bearing URLs are rejected"
	case errors.Is(err, secreport.ErrRedirectsForbidden):
		return "doctor: gateway redirects are not followed"
	case errors.Is(err, secreport.ErrGatewayRequest):
		return "doctor: gateway report request failed"
	default:
		return "doctor: security report failed"
	}
}
