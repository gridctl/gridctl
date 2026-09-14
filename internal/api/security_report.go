package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/secreport"
)

func (s *Server) handleSecurityReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.passiveSecurityReport(r.Context()))
}

func (s *Server) passiveSecurityReport(ctx context.Context) *secreport.Report {
	in := secreport.Inputs{
		Source: secreport.SourceIdentity{
			Kind:      secreport.SourceGateway,
			Display:   secreportDisplayHost(s.gatewayAddr),
			StackName: s.stackName,
		},
	}
	if s.stackFile != "" {
		if fileIn, err := secreport.InputsFromStackFile(ctx, s.stackFile); err == nil {
			in.Stack = fileIn.Stack
			in.Source.StackName = fileIn.Source.StackName
		}
	}
	scan := (*secreport.ScanDeclView)(nil)
	enabled := true
	if in.Stack != nil {
		scan = in.Stack.Scan
		if in.Stack.Pinning != nil {
			enabled = in.Stack.Pinning.Enabled
		}
	}
	in.Pins = secreport.PinViewFromStore(s.pinStore, enabled, scan)
	in.SkillPins = secreport.SkillPinViewFromStore(s.skillPinStore)
	if s.authType != "" || s.authToken != "" || s.gatewayAddr != "" {
		in.Startup = &secreport.StartupView{AuthEnabled: s.authToken != "", AuthType: s.authType, Bind: secreportDisplayHost(s.gatewayAddr)}
	}
	in.Execution = s.passiveExecutionViews()
	return secreport.Assemble(time.Now().UTC(), in)
}

func (s *Server) passiveExecutionViews() []secreport.ExecutionView {
	if s.gateway == nil {
		return nil
	}
	var out []secreport.ExecutionView
	for _, st := range s.gateway.Status() {
		kind := "container"
		switch {
		case st.OpenAPI:
			kind = "openapi"
		case st.SSH:
			kind = "ssh"
		case st.LocalProcess:
			kind = "local-process"
		case st.External:
			kind = "external"
		}
		if st.RegistrationFailed {
			if st.Execution != nil {
				out = append(out, executionViewFromReport(st.Name, "", kind, st.Execution))
			}
			continue
		}
		for _, replica := range st.Replicas {
			if replica.Execution == nil {
				continue
			}
			out = append(out, executionViewFromReport(st.Name, strconv.Itoa(replica.ReplicaID), kind, replica.Execution))
		}
	}
	return out
}

func executionViewFromReport(server, replica, kind string, rep *execution.Report) secreport.ExecutionView {
	if rep == nil {
		return secreport.ExecutionView{Server: server, Replica: replica, Kind: kind}
	}
	return secreport.ExecutionView{
		Server:     server,
		Replica:    replica,
		Kind:       kind,
		Mode:       rep.Mode,
		Outcome:    rep.Outcome,
		Eligible:   rep.Eligible,
		ObservedAt: rep.ObservedAt,
		Runtime:    rep.Runtime,
		Instance:   rep.Instance,
		Revision:   rep.Revision,
	}
}

func secreportDisplayHost(addr string) string {
	if addr == "" {
		return "gateway"
	}
	return addr
}
