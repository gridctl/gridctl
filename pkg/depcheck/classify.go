package depcheck

import (
	"strings"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
)

// Kind identifies the selector family a classifier result applies to.
type Kind string

const (
	KindImage Kind = "image"
	KindNPM   Kind = "npm"
	KindPyPI  Kind = "pypi"
	KindNone  Kind = "none"
)

// Status is the complete-assessment outcome of a literal selector.
//
// Policy callers must treat StatusNotAssessed as unknown and must not
// accept it. Advisory validation maps that status to informational
// reference-not-assessed findings and must not turn it into a structural
// error or a policy rejection.
type Status string

const (
	StatusMutable     Status = "mutable"
	StatusPinned      Status = "pinned"
	StatusNotAssessed Status = "not-assessed"
)

// Result is a pure classification of one literal selector. It never
// includes the selector text, command arguments, or credential material.
type Result struct {
	Kind   Kind
	Status Status
	Reason string
}

func notAssessed(kind Kind, reason string) Result {
	return Result{Kind: kind, Status: StatusNotAssessed, Reason: reason}
}

func ContainsVariable(s string) bool {
	return strings.Contains(s, "$")
}

// ClassifyImage classifies a literal OCI image reference. Version tags,
// including exact-looking tags and an absent tag, are mutable without a
// digest. A syntactically valid digest is content-addressed, not
// publisher-verified.
func ClassifyImage(selector string) Result {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return notAssessed(KindImage, "empty")
	}
	if ContainsVariable(selector) {
		return notAssessed(KindImage, "variable")
	}
	if candidate, ok := digestCandidate(selector); ok {
		if _, err := digest.Parse(candidate); err != nil {
			return notAssessed(KindImage, "invalid-digest")
		}
	}
	ref, err := reference.ParseAnyReference(selector)
	if err != nil {
		if _, ok := digestCandidate(selector); ok {
			return notAssessed(KindImage, "invalid-digest")
		}
		return notAssessed(KindImage, "unsupported-selector")
	}
	if _, ok := ref.(reference.Digested); ok {
		return Result{Kind: KindImage, Status: StatusPinned, Reason: "digest"}
	}
	return Result{Kind: KindImage, Status: StatusMutable, Reason: "mutable-tag"}
}

func digestCandidate(selector string) (string, bool) {
	if at := strings.LastIndex(selector, "@"); at >= 0 {
		if at == len(selector)-1 {
			return "", false
		}
		return digestAlgorithmCandidate(selector[at+1:])
	}
	return digestAlgorithmCandidate(selector)
}

func digestAlgorithmCandidate(candidate string) (string, bool) {
	algo, _, ok := strings.Cut(candidate, ":")
	if !ok || algo == "" {
		return "", false
	}
	switch strings.ToLower(algo) {
	case "sha256", "sha384", "sha512":
		return candidate, true
	default:
		return "", false
	}
}
