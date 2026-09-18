package secreport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

const (
	maxSnapshotBytes = 1 << 20
)

var (
	ErrSourceRequired     = errors.New("security source required")
	ErrSourceInvalid      = errors.New("invalid security source")
	ErrSourceUnsupported  = errors.New("unsupported security source")
	ErrSourceUnreadable   = errors.New("security source unreadable")
	ErrSourceInvalidFmt   = errors.New("unsupported or invalid security snapshot")
	ErrRedirectsForbidden = errors.New("redirects are not followed")
	ErrCredentialURL      = errors.New("credential-bearing URLs are rejected")
	ErrGatewayRequest     = errors.New("gateway report request failed")
)

// SourceRef is an explicit no-fallback source selection.
type SourceRef struct {
	Kind  string
	Value string
}

func ParseSource(raw string) (SourceRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SourceRef{}, ErrSourceRequired
	}
	kind, value, ok := strings.Cut(raw, ":")
	if !ok || kind == "" || value == "" {
		return SourceRef{}, ErrSourceInvalid
	}
	switch kind {
	case SourceFile, SourceSnapshot, SourceGateway:
		return SourceRef{Kind: kind, Value: value}, nil
	default:
		return SourceRef{}, ErrSourceUnsupported
	}
}

func Load(ctx context.Context, ref SourceRef, gateway HTTPDoer) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch ref.Kind {
	case SourceFile:
		in, err := InputsFromStackFile(ctx, ref.Value)
		if err != nil {
			return nil, err
		}
		return Assemble(nowUTC(), in), nil
	case SourceSnapshot:
		return LoadSnapshot(ctx, ref.Value)
	case SourceGateway:
		if gateway == nil {
			return nil, ErrGatewayRequest
		}
		return FetchGatewayReport(ctx, ref.Value, gateway)
	default:
		return nil, ErrSourceUnsupported
	}
}

func InputsFromStackFile(ctx context.Context, path string) (Inputs, error) {
	if err := ctx.Err(); err != nil {
		return Inputs{}, err
	}
	stack, err := config.ParseStackIndex(ctx, path)
	if err != nil {
		return Inputs{}, fmt.Errorf("%w", ErrSourceUnreadable)
	}
	return Inputs{
		Source: SourceIdentity{Kind: SourceFile, Display: displayPath(path), StackName: SanitizeIdentifier(stack.Name)},
		Stack:  stackViewFromConfig(stack),
	}, nil
}

func LoadSnapshot(ctx context.Context, path string) (*Report, error) {
	data, err := readBoundedFile(ctx, path, maxSnapshotBytes)
	if err != nil {
		return nil, err
	}
	report, err := parseSnapshot(data, true)
	if err != nil {
		return nil, err
	}
	report.Source.Kind = SourceSnapshot
	report.Source.Display = displayPath(path)
	report.Source.Historical = true
	report.Limitations = defaultLimitations(true)
	return report, nil
}

func ParseSnapshot(data []byte) (*Report, error) {
	return parseSnapshot(data, true)
}

func parseSnapshot(data []byte, untrusted bool) (*Report, error) {
	if len(data) == 0 || len(data) > maxSnapshotBytes {
		return nil, ErrSourceInvalidFmt
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var report Report
	if err := dec.Decode(&report); err != nil {
		return nil, ErrSourceInvalidFmt
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, ErrSourceInvalidFmt
	}
	if report.SchemaVersion != SchemaVersion {
		return nil, ErrSourceInvalidFmt
	}
	if err := validateImportedReport(&report); err != nil {
		return nil, err
	}
	return sanitizeReport(&report, untrusted), nil
}

func validateImportedReport(report *Report) error {
	if report.GeneratedAt.IsZero() {
		return ErrSourceInvalidFmt
	}
	if report.Source.Kind == "" {
		return ErrSourceInvalidFmt
	}
	seen := map[string]bool{}
	for _, c := range report.Checks {
		if c.ID == "" || seen[c.ID] {
			return ErrSourceInvalidFmt
		}
		seen[c.ID] = true
		if !knownPredicate(c.Predicate) || !knownOutcome(c.Outcome) || !knownReasonCode(c.ReasonCode) {
			return ErrSourceInvalidFmt
		}
		if c.Subject.Kind != "" && !knownSubjectKind(c.Subject.Kind) {
			return ErrSourceInvalidFmt
		}
		if c.Evidence.Basis != "" && !knownBasis(c.Evidence.Basis) {
			return ErrSourceInvalidFmt
		}
		if c.Evidence.Availability != "" && !knownAvailability(c.Evidence.Availability) {
			return ErrSourceInvalidFmt
		}
		if c.Evidence.Freshness != "" && !knownFreshness(c.Evidence.Freshness) {
			return ErrSourceInvalidFmt
		}
		if c.Outcome == OutcomeFail && !failPredicateAllowed(c.Predicate) {
			return ErrSourceInvalidFmt
		}
		if c.Evidence.Basis == BasisVerified && (c.Evidence.VerificationMethod == "" || c.Evidence.SubjectBinding == "" || c.Evidence.PredicateScope == "") {
			return ErrSourceInvalidFmt
		}
	}
	return nil
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func FetchGatewayReport(ctx context.Context, base string, doer HTTPDoer) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, err := gatewayReportURL(base)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrGatewayRequest
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, ErrGatewayRequest
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, ErrGatewayRequest
	}
	limited := io.LimitReader(resp.Body, maxSnapshotBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > maxSnapshotBytes {
		return nil, ErrSourceInvalidFmt
	}
	report, err := parseSnapshot(data, false)
	if err != nil {
		return nil, err
	}
	report.Source.Kind = SourceGateway
	report.Source.Display = displayGatewayBase(base)
	report.Source.Historical = false
	report.Limitations = defaultLimitations(false)
	return report, nil
}

func gatewayReportURL(base string) (string, error) {
	u, err := neturl.Parse(strings.TrimSpace(base))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ErrSourceInvalid
	}
	if u.User != nil {
		return "", ErrCredentialURL
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", ErrSourceInvalid
	}
	u.Path = "/api/security-report"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String(), nil
}

func RedirectCheck(_ *http.Request, _ []*http.Request) error {
	return ErrRedirectsForbidden
}

func stackViewFromConfig(stack *config.Stack) *StackView {
	if stack == nil {
		return nil
	}
	view := &StackView{
		Name:    SanitizeIdentifier(stack.Name),
		Gateway: gatewayDecl(stack),
		Scan:    scanDecl(stack),
		Pinning: pinningDecl(stack),
	}
	for _, srv := range stack.MCPServers {
		view.Servers = append(view.Servers, serverViewFromConfig(srv))
	}
	if stack.Secrets != nil && len(stack.Secrets.Sets) > 0 {
		view.SetMembers = nil
		for _, ref := range stack.Secrets.Sets {
			if !ref.Scoped() {
				view.UnscopedSetCount++
			}
		}
	} else {
		view.SetMembers = map[string][]string{}
	}
	usage, complete := config.BuildVariableUsage(stack, view.SetMembers)
	view.References = referenceSitesFromUsage(usage)
	if !complete {
		view.SetMembers = nil
	}
	return view
}

func authoredIdentity(value string) string {
	if value == "" || config.ContainsExpansion(value) {
		return ""
	}
	return SanitizeIdentifier(value)
}

func serverViewFromConfig(srv config.MCPServer) ServerView {
	view := ServerView{
		Name: SanitizeIdentifier(srv.Name),
		Kind: serverKind(srv),
	}
	view.Image = authoredIdentity(srv.Image)
	if srv.Source != nil {
		view.SourceType = authoredIdentity(srv.Source.Type)
		view.SourcePackage = authoredIdentity(srv.Source.Package)
		view.SourceVersion = authoredIdentity(srv.Source.Ref)
		view.SourceRef = authoredIdentity(srv.Source.Ref)
		if !config.ContainsExpansion(srv.Source.URL) {
			if hostPath, ok := sourceURLIdentity(srv.Source.URL); ok {
				view.SourceHostPath = hostPath
			}
		}
	}
	if srv.Execution != nil {
		view.ExecutionDeclared = true
		view.ExecutionMode = SanitizeIdentifier(srv.Execution.Mode)
	}
	return view
}

func serverKind(srv config.MCPServer) string {
	switch {
	case srv.IsA2A():
		return "a2a"
	case srv.IsOpenAPI():
		return "openapi"
	case srv.IsSSH():
		return "ssh"
	case srv.IsLocalProcess():
		return "local-process"
	case srv.IsExternal():
		return "external"
	default:
		return "container"
	}
}

func referenceSitesFromUsage(usage config.ReferenceIndex) map[string][]ReferenceSite {
	out := map[string][]ReferenceSite{}
	for key, consumers := range usage {
		k := SanitizeIdentifier(key)
		if k == "" {
			continue
		}
		for _, c := range consumers {
			site := ReferenceSite{
				Kind:       SanitizeIdentifier(string(c.Kind)),
				Name:       SanitizeIdentifier(c.Name),
				Target:     SanitizeIdentifier(c.Target),
				TargetKind: SanitizeIdentifier(string(c.TargetKind)),
			}
			if c.Kind == config.RefKindSecretsSet && c.Target == "" {
				site.Untargeted = true
			}
			out[k] = append(out[k], site)
		}
	}
	return out
}

func gatewayDecl(stack *config.Stack) *GatewayDeclView {
	if stack == nil || stack.Gateway == nil {
		return &GatewayDeclView{}
	}
	decl := &GatewayDeclView{
		Bind:     authoredIdentity(stack.Gateway.Bind),
		Insecure: stack.Gateway.InsecureAllowUnauthenticated,
	}
	if stack.Gateway.Auth != nil && stack.Gateway.Auth.Token != "" {
		decl.AuthDeclared = true
		decl.AuthType = SanitizeIdentifier(stack.Gateway.Auth.Type)
	}
	return decl
}

func scanDecl(stack *config.Stack) *ScanDeclView {
	enabled := true
	var ignore []string
	if stack != nil && stack.Gateway != nil && stack.Gateway.Security != nil && stack.Gateway.Security.SchemaPinning != nil {
		cfg := stack.Gateway.Security.SchemaPinning
		if cfg.Scan != nil {
			enabled = *cfg.Scan
		}
		for _, code := range cfg.ScanIgnore {
			if c := SanitizeIdentifier(code); c != "" {
				ignore = append(ignore, c)
			}
		}
	}
	return &ScanDeclView{Enabled: enabled, Ignore: ignore}
}

func pinningDecl(stack *config.Stack) *PinningDeclView {
	enabled := true
	if stack != nil && stack.Gateway != nil && stack.Gateway.Security != nil && stack.Gateway.Security.SchemaPinning != nil && stack.Gateway.Security.SchemaPinning.Enabled != nil {
		enabled = *stack.Gateway.Security.SchemaPinning.Enabled
	}
	return &PinningDeclView{Enabled: enabled}
}

func PinViewFromStore(ps *pins.PinStore, enabled bool, scan *ScanDeclView) *PinView {
	if ps == nil {
		return nil
	}
	view := &PinView{Available: true, Enabled: enabled, Scan: scan, Servers: map[string]ServerPinView{}}
	for name, rec := range ps.GetAll() {
		if rec == nil {
			continue
		}
		view.Servers[SanitizeIdentifier(name)] = serverPinView(rec, scan)
	}
	return view
}

func serverPinView(rec *pins.ServerPins, scan *ScanDeclView) ServerPinView {
	view := ServerPinView{
		Status:         SanitizeIdentifier(rec.Status),
		ToolCount:      rec.ToolCount,
		PinnedAt:       rec.PinnedAt.UTC(),
		LastVerifiedAt: rec.LastVerifiedAt.UTC(),
	}
	ignore := map[string]bool{}
	if scan != nil {
		for _, code := range scan.Ignore {
			ignore[code] = true
		}
	}
	for _, tool := range rec.Tools {
		if tool == nil {
			continue
		}
		if strings.HasPrefix(tool.Hash, "h2:") {
			view.CurrentSchemeCount++
		} else if tool.Hash != "" {
			view.LegacySchemeCount++
		}
		for _, finding := range tool.Findings {
			view.Findings = append(view.Findings, FindingView{
				Code:       SanitizeIdentifier(finding.Code),
				Severity:   SanitizeIdentifier(finding.Severity),
				Confidence: SanitizeIdentifier(finding.Confidence),
				Field:      SanitizeIdentifier(finding.Field),
				Suppressed: ignore[finding.Code],
			})
		}
	}
	return view
}

func SkillPinViewFromStore(ps *skillpins.Store) *SkillPinView {
	if ps == nil {
		return nil
	}
	view := &SkillPinView{Available: true, Skills: map[string]SkillPinRecordView{}}
	for name, rec := range ps.GetAll() {
		if rec == nil {
			continue
		}
		item := SkillPinRecordView{
			Status:         SanitizeIdentifier(rec.Status),
			Source:         SanitizeIdentifier(rec.Source),
			PinnedAt:       rec.PinnedAt.UTC(),
			LastVerifiedAt: rec.LastVerifiedAt.UTC(),
		}
		for _, finding := range rec.Findings {
			item.FindingCodes = append(item.FindingCodes, SanitizeIdentifier(finding.Code))
		}
		view.Skills[SanitizeIdentifier(name)] = item
	}
	return view
}
