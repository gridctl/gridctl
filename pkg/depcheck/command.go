package depcheck

import (
	"path/filepath"
	"regexp"
	"strings"
)

var exactNpmVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

var exactPyPIVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([.-][0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

var packageLaunchers = map[string]bool{
	"npx":  true,
	"uvx":  true,
	"npm":  true,
	"yarn": true,
	"pnpm": true,
	"bun":  true,
	"bunx": true,
	"pipx": true,
	"uv":   true,
	"pip":  true,
}

// IsPackageLauncher reports whether argv[0] is a documented or related
// package launcher. Unknown host binaries are not package selectors.
func IsPackageLauncher(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return packageLaunchers[launcherName(argv[0])]
}

func launcherName(s string) string {
	return strings.ToLower(filepath.Base(strings.TrimSpace(s)))
}

// ClassifyCommand classifies a bounded npx or uvx argv. Other wrappers,
// unsupported options, local paths, and dynamic commands are not assessed.
func ClassifyCommand(argv []string) Result {
	if len(argv) == 0 {
		return notAssessed(KindNone, "empty")
	}
	if ContainsVariable(argv[0]) {
		return notAssessed(KindNone, "variable")
	}
	name := launcherName(argv[0])
	switch name {
	case "npx":
		return classifyNpx(argv[1:])
	case "uvx":
		return classifyUvx(argv[1:])
	default:
		if packageLaunchers[name] {
			return notAssessed(KindNone, "unsupported-launcher")
		}
		return notAssessed(KindNone, "unknown-wrapper")
	}
}

func classifyNpx(args []string) Result {
	var specs []string
	fromPackageFlag := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			switch {
			case a == "-y" || a == "--yes" || a == "--no-yes" || a == "-q" || a == "--quiet":
				continue
			case a == "-c" || a == "--call":
				return notAssessed(KindNPM, "unsupported-option")
			case a == "-p" || a == "--package":
				next, ok := peekArg(args, i)
				if !ok {
					return notAssessed(KindNPM, "unknown-form")
				}
				i++
				if ContainsVariable(next) {
					return notAssessed(KindNPM, "variable")
				}
				specs = append(specs, next)
				fromPackageFlag = true
			case strings.HasPrefix(a, "--package="):
				spec := strings.TrimPrefix(a, "--package=")
				if ContainsVariable(spec) {
					return notAssessed(KindNPM, "variable")
				}
				specs = append(specs, spec)
				fromPackageFlag = true
			default:
				return notAssessed(KindNPM, "unsupported-option")
			}
			continue
		}
		if ContainsVariable(a) {
			return notAssessed(KindNPM, "variable")
		}
		if !fromPackageFlag {
			specs = append(specs, a)
		}
		break
	}
	if len(specs) == 0 {
		return notAssessed(KindNPM, "unknown-form")
	}
	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		results = append(results, classifyNpmSpec(spec))
	}
	return combineResults(results)
}

func classifyUvx(args []string) Result {
	fromSpec := ""
	positional := ""
	var extras []Result
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			switch {
			case a == "--from":
				next, ok := peekArg(args, i)
				if !ok {
					return notAssessed(KindPyPI, "unknown-form")
				}
				i++
				if ContainsVariable(next) {
					return notAssessed(KindPyPI, "variable")
				}
				fromSpec = next
			case strings.HasPrefix(a, "--from="):
				fromSpec = strings.TrimPrefix(a, "--from=")
				if ContainsVariable(fromSpec) {
					return notAssessed(KindPyPI, "variable")
				}
			case a == "--python" || a == "-p":
				if _, ok := peekArg(args, i); !ok {
					return notAssessed(KindPyPI, "unknown-form")
				}
				i++
			case strings.HasPrefix(a, "--python="):
				// interpreter, not a package selector
			case a == "--with" || a == "--with-editable":
				next, ok := peekArg(args, i)
				if !ok {
					return notAssessed(KindPyPI, "unknown-form")
				}
				i++
				if ContainsVariable(next) {
					return notAssessed(KindPyPI, "variable")
				}
				extras = append(extras, classifyPyPISpec(next))
			case strings.HasPrefix(a, "--with="):
				spec := strings.TrimPrefix(a, "--with=")
				if ContainsVariable(spec) {
					return notAssessed(KindPyPI, "variable")
				}
				extras = append(extras, classifyPyPISpec(spec))
			case strings.HasPrefix(a, "--with-editable="):
				spec := strings.TrimPrefix(a, "--with-editable=")
				if ContainsVariable(spec) {
					return notAssessed(KindPyPI, "variable")
				}
				extras = append(extras, classifyPyPISpec(spec))
			case a == "--isolated" || a == "--no-cache" || a == "--refresh" || a == "-q" || a == "--quiet":
				// skip
			default:
				return notAssessed(KindPyPI, "unsupported-option")
			}
			continue
		}
		if positional == "" {
			if ContainsVariable(a) {
				return notAssessed(KindPyPI, "variable")
			}
			positional = a
			break
		}
	}
	spec := fromSpec
	if spec == "" {
		spec = positional
	}
	var results []Result
	if spec != "" {
		results = append(results, classifyPyPISpec(spec))
	}
	results = append(results, extras...)
	if len(results) == 0 {
		return notAssessed(KindPyPI, "unknown-form")
	}
	return combineResults(results)
}

func classifyNpmSpec(spec string) Result {
	spec = strings.TrimSpace(spec)
	spec = strings.Trim(spec, `"'`)
	if spec == "" {
		return notAssessed(KindNPM, "unknown-form")
	}
	if isLocalOrURL(spec) {
		return notAssessed(KindNPM, "local-path")
	}
	name, version, ok := splitNpmSpec(spec)
	if !ok || name == "" {
		return notAssessed(KindNPM, "unknown-form")
	}
	if version == "" {
		return Result{Kind: KindNPM, Status: StatusMutable, Reason: "missing-version"}
	}
	if exactNpmVersion.MatchString(version) {
		return Result{Kind: KindNPM, Status: StatusPinned, Reason: "exact-version"}
	}
	return Result{Kind: KindNPM, Status: StatusMutable, Reason: "floating-range"}
}

func splitNpmSpec(spec string) (name, version string, ok bool) {
	if strings.HasPrefix(spec, "@") {
		slash := strings.IndexByte(spec, '/')
		if slash < 0 {
			return "", "", false
		}
		rest := spec[slash+1:]
		if at := strings.LastIndexByte(rest, '@'); at >= 0 {
			return spec[:slash+1+at], rest[at+1:], true
		}
		return spec, "", true
	}
	if at := strings.LastIndexByte(spec, '@'); at >= 0 {
		return spec[:at], spec[at+1:], true
	}
	return spec, "", true
}

func classifyPyPISpec(spec string) Result {
	spec = strings.TrimSpace(spec)
	spec = strings.Trim(spec, `"'`)
	if spec == "" {
		return notAssessed(KindPyPI, "unknown-form")
	}
	if strings.Contains(spec, ";") {
		return notAssessed(KindPyPI, "unknown-form")
	}
	if isLocalOrURL(spec) {
		return notAssessed(KindPyPI, "local-path")
	}
	name, op, version := splitPEP508(spec)
	if name == "" {
		return notAssessed(KindPyPI, "unknown-form")
	}
	if op == "" {
		return Result{Kind: KindPyPI, Status: StatusMutable, Reason: "missing-version"}
	}
	if op == "==" && exactPyPIVersion.MatchString(version) {
		return Result{Kind: KindPyPI, Status: StatusPinned, Reason: "exact-version"}
	}
	return Result{Kind: KindPyPI, Status: StatusMutable, Reason: "floating-range"}
}

func splitPEP508(spec string) (name, op, version string) {
	if i := strings.IndexByte(spec, '['); i >= 0 {
		if j := strings.IndexByte(spec[i:], ']'); j >= 0 {
			spec = spec[:i] + spec[i+j+1:]
		} else {
			return "", "", ""
		}
	}
	ops := []string{"===", "==", "!=", "<=", ">=", "~=", "<", ">"}
	for _, candidate := range ops {
		if i := strings.Index(spec, candidate); i >= 0 {
			return strings.TrimSpace(spec[:i]), candidate, strings.TrimSpace(spec[i+len(candidate):])
		}
	}
	return strings.TrimSpace(spec), "", ""
}

func combineResults(results []Result) Result {
	if len(results) == 0 {
		return notAssessed(KindNone, "unknown-form")
	}
	var mutable, unassessed *Result
	for i := range results {
		r := &results[i]
		switch r.Status {
		case StatusMutable:
			if mutable == nil {
				mutable = r
			}
		case StatusNotAssessed:
			if unassessed == nil {
				unassessed = r
			}
		}
	}
	if mutable != nil {
		return *mutable
	}
	if unassessed != nil {
		return *unassessed
	}
	return results[0]
}

func peekArg(args []string, i int) (string, bool) {
	if i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func isLocalOrURL(spec string) bool {
	switch {
	case spec == ".", spec == "..":
		return true
	case strings.HasPrefix(spec, "./"), strings.HasPrefix(spec, "../"), strings.HasPrefix(spec, "/"), strings.HasPrefix(spec, "~/"):
		return true
	case strings.HasPrefix(spec, "file:"), strings.HasPrefix(spec, "git+"), strings.HasPrefix(spec, "hg+"), strings.HasPrefix(spec, "svn+"):
		return true
	case strings.Contains(spec, "://"):
		return true
	default:
		return false
	}
}
