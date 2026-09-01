package builder

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pythonVersionPattern = regexp.MustCompile(`(?i)^v?(?:(\d+)!)?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:(a|b|c|rc|alpha|beta|pre|preview)[._-]?(\d*))?(?:(?:-(\d+))|(?:[._-]?(post|rev|r)[._-]?(\d*)))?(?:[._-]?dev[._-]?(\d*))?(?:\+[a-z0-9]+(?:[._-][a-z0-9]+)*)?$`)

type pythonVersion struct {
	epoch int
	major int
	minor int
	patch int
	stage int
}

// SelectPythonVersion chooses a supported interpreter that satisfies a
// requires-python specifier. An explicit selection is validated rather than
// replaced.
func SelectPythonVersion(requiresPython, explicit string) (string, error) {
	candidates := []struct {
		minor string
		patch string
	}{
		{"3.10", "3.10.18"},
		{"3.11", "3.11.13"},
		{"3.12", "3.12.11"},
		{"3.13", "3.13.7"},
	}
	if explicit != "" {
		selectedPatch := ""
		for _, candidate := range candidates {
			if explicit == candidate.minor {
				selectedPatch = candidate.patch
				break
			}
		}
		if selectedPatch != "" && versionSatisfies(selectedPatch, requiresPython) {
			return explicit, nil
		}
		return "", incompatiblePythonError(requiresPython, explicit)
	}
	if strings.TrimSpace(requiresPython) == "" {
		return "3.12", nil
	}
	for _, candidate := range candidates {
		if versionSatisfies(candidate.patch, requiresPython) {
			return candidate.minor, nil
		}
	}
	return "", incompatiblePythonError(requiresPython, "3.10 through 3.13")
}

func incompatiblePythonError(specifier, selection string) error {
	//nolint:staticcheck // The public Error Contract requires this punctuation.
	return fmt.Errorf("Package requires Python %s, which is incompatible with image selection %s. Set source.python to a compatible version from 3.10 through 3.13, or use a custom Dockerfile.", specifier, selection)
}

func versionSatisfies(value, specifier string) bool {
	specifier = strings.TrimSpace(specifier)
	if strings.HasPrefix(specifier, "(") && strings.HasSuffix(specifier, ")") {
		specifier = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(specifier, "("), ")"))
	}
	if strings.TrimSpace(specifier) == "" {
		return true
	}
	version, ok := parsePythonVersion(value)
	if !ok {
		return false
	}
	for _, raw := range strings.Split(specifier, ",") {
		raw = strings.TrimSpace(raw)
		op := "=="
		for _, candidate := range []string{"===", "~=", "==", "!=", "<=", ">=", "<", ">"} {
			if strings.HasPrefix(raw, candidate) {
				op = candidate
				raw = strings.TrimSpace(strings.TrimPrefix(raw, candidate))
				break
			}
		}
		wildcard := strings.HasSuffix(raw, ".*")
		wildcardParts := strings.Split(strings.TrimSuffix(raw, ".*"), ".")
		if op == "===" {
			if value != raw {
				return false
			}
			continue
		}
		other, valid := parsePythonVersion(strings.TrimSuffix(raw, ".*"))
		if !valid {
			return false
		}
		cmp := comparePythonVersions(version, other)
		matches := false
		switch op {
		case "==":
			matches = cmp == 0 || wildcard && wildcardVersionMatch(version, other, len(wildcardParts))
		case "!=":
			matches = cmp != 0 && (!wildcard || !wildcardVersionMatch(version, other, len(wildcardParts)))
		case "<=":
			matches = cmp <= 0
		case ">=":
			matches = cmp >= 0
		case "<":
			matches = cmp < 0
		case ">":
			matches = cmp > 0
		case "~=":
			upper := pythonVersion{major: other.major + 1}
			if strings.Count(raw, ".") >= 2 {
				upper = pythonVersion{major: other.major, minor: other.minor + 1}
			}
			matches = cmp >= 0 && comparePythonVersions(version, upper) < 0
		}
		if !matches {
			return false
		}
	}
	return true
}

func wildcardVersionMatch(value, prefix pythonVersion, parts int) bool {
	if value.epoch != prefix.epoch || value.major != prefix.major {
		return false
	}
	if parts > 1 && value.minor != prefix.minor {
		return false
	}
	return parts <= 2 || value.patch == prefix.patch
}

func parsePythonVersion(value string) (pythonVersion, bool) {
	match := pythonVersionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return pythonVersion{}, false
	}
	parts := [4]int{}
	for i := range parts {
		if match[i+1] != "" {
			parts[i], _ = strconv.Atoi(match[i+1])
		}
	}
	stage := 0
	if match[5] != "" || match[10] != "" {
		stage = -1
	} else if match[7] != "" || match[8] != "" {
		stage = 1
	}
	return pythonVersion{epoch: parts[0], major: parts[1], minor: parts[2], patch: parts[3], stage: stage}, true
}

func comparePythonVersions(a, b pythonVersion) int {
	if a.epoch != b.epoch {
		return a.epoch - b.epoch
	}
	if a.major != b.major {
		return a.major - b.major
	}
	if a.minor != b.minor {
		return a.minor - b.minor
	}
	if a.patch != b.patch {
		return a.patch - b.patch
	}
	return a.stage - b.stage
}
