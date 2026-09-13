package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

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
	if s.authType != "" || s.authToken != "" {
		in.Startup = &secreport.StartupView{AuthEnabled: s.authToken != "", AuthType: s.authType}
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
		for _, replica := range st.Replicas {
			if replica.Execution == nil {
				continue
			}
			out = append(out, secreport.ExecutionView{
				Server:     st.Name,
				Replica:    strconv.Itoa(replica.ReplicaID),
				Kind:       kind,
				Mode:       replica.Execution.Mode,
				Outcome:    replica.Execution.Outcome,
				Eligible:   replica.Execution.Eligible,
				ObservedAt: replica.Execution.ObservedAt,
				Runtime:    replica.Execution.Runtime,
				Instance:   replica.Execution.Instance,
				Revision:   replica.Execution.Revision,
			})
		}
	}
	return out
}

func secreportDisplayHost(addr string) string {
	if addr == "" {
		return "gateway"
	}
	return addr
}
