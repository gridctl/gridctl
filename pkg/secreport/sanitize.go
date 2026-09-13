package secreport

import (
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const identifierMaxRunes = 128

func SanitizeIdentifier(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	count := 0
	for _, r := range value {
		if r < 32 || r == 127 || !unicode.IsPrint(r) {
			continue
		}
		switch r {
		case '<', '>', '"', '\'', '`', '\\', '?', '#', '&', '=', '%', '{', '}', '[', ']', '|', ';':
			continue
		}
		if count >= identifierMaxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}

func SanitizeDigest(value string) string {
	value = SanitizeIdentifier(value)
	if value == "" {
		return ""
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ':' || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return value
}

func displayPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 && i+1 < len(path) {
		path = path[i+1:]
	}
	return SanitizeIdentifier(path)
}

func displayGatewayBase(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return SanitizeIdentifier(raw)
	}
	host := u.Hostname()
	port := u.Port()
	if port != "" {
		return SanitizeIdentifier(host + ":" + port)
	}
	return SanitizeIdentifier(host)
}

func sourceURLIdentity(raw string) (hostPath string, ok bool) {
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.User != nil {
		return "", false
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := u.Host
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if path == "" {
		return SanitizeIdentifier(host), true
	}
	return SanitizeIdentifier(host + path), true
}

func viewPinsAction(server string) *Action {
	name := SanitizeIdentifier(server)
	if name == "" || !safeActionName(name) {
		return nil
	}
	return &Action{ID: ActionViewPins, Label: "View pins", Path: "/pins?server=" + url.QueryEscape(name)}
}

func viewSkillPinsAction(skill string) *Action {
	name := SanitizeIdentifier(skill)
	if name == "" || !safeActionName(name) {
		return nil
	}
	return &Action{ID: ActionViewSkillPins, Label: "View skill pins", Path: "/pins?kind=skill&skill=" + url.QueryEscape(name)}
}

func safeActionName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' || r == ':' {
			continue
		}
		return false
	}
	return utf8.RuneCountInString(name) <= identifierMaxRunes
}

func defaultLimitations(historical bool) []string {
	out := []string{
		"This report is not a certification, trust score, or complete security assessment.",
		"Exit zero means no established failures among documented fail predicates, not that the stack is secure.",
		"Allowlisted identifiers may still contain operator-authored secrets; this passive report cannot detect them.",
		"Refresh re-reads this passive report; it does not re-observe downstream evidence.",
		"Truncation bounds identifier length; it is not redaction.",
	}
	if historical {
		out = append(out, "This rendering is supplied historical evidence, not a fresh verification.")
	}
	return out
}

func copyTime(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	cp := t.UTC()
	return &cp
}
