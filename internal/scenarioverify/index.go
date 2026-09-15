package scenarioverify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	IndexVersion  = 1
	maxIndexBytes = 1 << 20
)

type Index struct {
	Version    int        `yaml:"version"`
	Lanes      []string   `yaml:"lanes"`
	Scenarios  []Scenario `yaml:"scenarios"`
	laneSet    map[string]struct{}
	byLane     map[string][]Scenario
}

type Scenario struct {
	ID               string   `yaml:"id"`
	OwnerPackage     string   `yaml:"owner_package"`
	OwnerIssue       string   `yaml:"owner_issue"`
	Package          string   `yaml:"package"`
	Test             string   `yaml:"test"`
	Lanes            []string `yaml:"lanes"`
	Prerequisites    []string `yaml:"prerequisites"`
	ExpectedBoundary string   `yaml:"expected_boundary"`
	Nonclaims        []string `yaml:"nonclaims"`
	Budgets          Budgets  `yaml:"budgets"`
}

type Budgets struct {
	MaxPayloadBytes int    `yaml:"max_payload_bytes"`
	MaxDuration     string `yaml:"max_duration"`
}

func LoadIndex(ctx context.Context, path string) (*Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("%s: index path is required", ReasonMalformedIndex)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ReasonMalformedIndex, err)
	}
	if info.Size() > maxIndexBytes {
		return nil, fmt.Errorf("%s: index exceeds %d bytes", ReasonMalformedIndex, maxIndexBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ReasonMalformedIndex, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxIndexBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ReasonMalformedIndex, err)
	}
	if int64(len(data)) > maxIndexBytes {
		return nil, fmt.Errorf("%s: index exceeds %d bytes", ReasonMalformedIndex, maxIndexBytes)
	}
	return ParseIndex(ctx, data)
}

func ParseIndex(ctx context.Context, data []byte) (*Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var idx Index
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("%s: %w", ReasonMalformedIndex, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%s: concatenated documents are not allowed", ReasonMalformedIndex)
		}
		return nil, fmt.Errorf("%s: %w", ReasonMalformedIndex, err)
	}
	if err := idx.validate(); err != nil {
		return nil, err
	}
	return &idx, nil
}

func (idx *Index) validate() error {
	if idx.Version != IndexVersion {
		return fmt.Errorf("%s: unsupported version %d", ReasonMalformedIndex, idx.Version)
	}
	if len(idx.Lanes) == 0 {
		return fmt.Errorf("%s: lanes must be a non-empty list", ReasonMalformedIndex)
	}
	idx.laneSet = make(map[string]struct{}, len(idx.Lanes))
	for _, lane := range idx.Lanes {
		if !validIdent(lane) {
			return fmt.Errorf("%s: invalid lane %q", ReasonMalformedIndex, lane)
		}
		if _, dup := idx.laneSet[lane]; dup {
			return fmt.Errorf("%s: duplicate lane %q", ReasonMalformedIndex, lane)
		}
		idx.laneSet[lane] = struct{}{}
	}
	if len(idx.Scenarios) == 0 {
		return fmt.Errorf("%s: scenarios must be a non-empty list", ReasonMalformedIndex)
	}
	ids := make(map[string]struct{}, len(idx.Scenarios))
	selectors := make(map[string]struct{}, len(idx.Scenarios))
	idx.byLane = make(map[string][]Scenario, len(idx.Lanes))
	for i, sc := range idx.Scenarios {
		if !validIdent(sc.ID) {
			return fmt.Errorf("%s: invalid scenario id %q", ReasonMalformedIndex, sc.ID)
		}
		if _, dup := ids[sc.ID]; dup {
			return fmt.Errorf("%s: duplicate scenario id %q", ReasonMalformedIndex, sc.ID)
		}
		ids[sc.ID] = struct{}{}
		if sc.OwnerPackage == "" || sc.OwnerIssue == "" {
			return fmt.Errorf("%s: scenario %q missing owner", ReasonMalformedIndex, sc.ID)
		}
		if sc.Package == "" || sc.Test == "" {
			return fmt.Errorf("%s: scenario %q missing package or test", ReasonMalformedIndex, sc.ID)
		}
		if strings.ContainsAny(sc.Test, ".*+?()[]{}|\\") {
			return fmt.Errorf("%s: scenario %q test identity must be exact, not a regex", ReasonMalformedIndex, sc.ID)
		}
		key := sc.Package + "\x00" + sc.Test
		if _, dup := selectors[key]; dup {
			return fmt.Errorf("%s: duplicate selector %s %s", ReasonMalformedIndex, sc.Package, sc.Test)
		}
		selectors[key] = struct{}{}
		if len(sc.Lanes) == 0 {
			return fmt.Errorf("%s: scenario %q has no lanes", ReasonMalformedIndex, sc.ID)
		}
		seenLane := make(map[string]struct{}, len(sc.Lanes))
		for _, lane := range sc.Lanes {
			if _, ok := idx.laneSet[lane]; !ok {
				return fmt.Errorf("%s: scenario %q references unknown lane %q", ReasonMalformedIndex, sc.ID, lane)
			}
			if _, dup := seenLane[lane]; dup {
				return fmt.Errorf("%s: scenario %q duplicates lane %q", ReasonMalformedIndex, sc.ID, lane)
			}
			seenLane[lane] = struct{}{}
			idx.byLane[lane] = append(idx.byLane[lane], idx.Scenarios[i])
		}
		if strings.TrimSpace(sc.ExpectedBoundary) == "" {
			return fmt.Errorf("%s: scenario %q missing expected_boundary", ReasonMalformedIndex, sc.ID)
		}
		if sc.Budgets.MaxPayloadBytes < 0 {
			return fmt.Errorf("%s: scenario %q has negative payload budget", ReasonMalformedIndex, sc.ID)
		}
		if sc.Budgets.MaxDuration != "" {
			if _, err := time.ParseDuration(sc.Budgets.MaxDuration); err != nil {
				return fmt.Errorf("%s: scenario %q invalid max_duration: %w", ReasonMalformedIndex, sc.ID, err)
			}
		}
	}
	for _, lane := range idx.Lanes {
		if len(idx.byLane[lane]) == 0 {
			return fmt.Errorf("%s: lane %q has no required scenarios", ReasonEmptyRequiredSet, lane)
		}
	}
	return nil
}

func (idx *Index) Required(lane string) ([]Scenario, error) {
	if _, ok := idx.laneSet[lane]; !ok {
		return nil, fmt.Errorf("%s: %s", ReasonUnknownLane, lane)
	}
	required := idx.byLane[lane]
	if len(required) == 0 {
		return nil, fmt.Errorf("%s: lane %q has no required scenarios", ReasonEmptyRequiredSet, lane)
	}
	out := make([]Scenario, len(required))
	copy(out, required)
	return out, nil
}

func (idx *Index) HasLane(lane string) bool {
	_, ok := idx.laneSet[lane]
	return ok
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if unicode.IsLetter(r) || r == '_' {
			continue
		}
		if i > 0 && (unicode.IsDigit(r) || r == '-' || r == '.') {
			continue
		}
		return false
	}
	return true
}
