package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	api := newDaemonAPIForBaseURL(base, 15*time.Second)
	api.client.CheckRedirect = secreport.RedirectCheck
	api.client.Timeout = 15 * time.Second
	return authorizedDoer{api: api}
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
	secreport.WriteText(w, report, quiet)
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
