package scenarioverify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const (
	defaultMaxInputBytes  = 512 << 20
	defaultMaxRecordBytes = 32 << 20
	defaultMaxEvents      = 2_000_000
	defaultMaxIdentities  = 100_000
)

var (
	errNotObject     = errors.New("event is not a JSON object")
	errMissingAction = errors.New("event is missing Action")
	errInputLimit    = errors.New("capture exceeds input limit")
	errRecordLimit   = errors.New("event exceeds record limit")
	errEventLimit    = errors.New("capture exceeds event limit")
	errIdentityLimit = errors.New("capture exceeds identity limit")
	errInvalidEvent  = errors.New("event is missing required fields")
	errCaptureDone   = errors.New("capture contract decided")
)

type DecodeLimits struct {
	MaxInputBytes  int64
	MaxRecordBytes int64
	MaxEvents      int
	MaxIdentities  int
}

func (l DecodeLimits) withDefaults() DecodeLimits {
	if l.MaxInputBytes <= 0 {
		l.MaxInputBytes = defaultMaxInputBytes
	}
	if l.MaxRecordBytes <= 0 {
		l.MaxRecordBytes = defaultMaxRecordBytes
	}
	if l.MaxEvents <= 0 {
		l.MaxEvents = defaultMaxEvents
	}
	if l.MaxIdentities <= 0 {
		l.MaxIdentities = defaultMaxIdentities
	}
	return l
}

type Event struct {
	Time        time.Time `json:"Time"`
	Action      string    `json:"Action"`
	Package     string    `json:"Package"`
	Test        string    `json:"Test"`
	Elapsed     *float64  `json:"Elapsed"`
	Output      string    `json:"Output"`
	Failed      bool      `json:"Failed"`
	ImportPath  string    `json:"ImportPath"`
	FailedBuild string    `json:"FailedBuild"`
}

func forEachEvent(ctx context.Context, r io.Reader, limits DecodeLimits, fn func(Event) error) error {
	limits = limits.withDefaults()
	dec := json.NewDecoder(io.LimitReader(r, limits.MaxInputBytes+1))
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				if dec.InputOffset() > limits.MaxInputBytes {
					return errInputLimit
				}
				return nil
			}
			if dec.InputOffset() > limits.MaxInputBytes {
				return errInputLimit
			}
			return err
		}
		if dec.InputOffset() > limits.MaxInputBytes {
			return errInputLimit
		}
		n++
		if n > limits.MaxEvents {
			return errEventLimit
		}
		raw = bytes.TrimSpace(raw)
		if int64(len(raw)) > limits.MaxRecordBytes {
			return errRecordLimit
		}
		if len(raw) == 0 || raw[0] != '{' {
			return errNotObject
		}
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			return err
		}
		ev.Output = ""
		ev.Action = strings.ToLower(strings.TrimSpace(ev.Action))
		if ev.Action == "" {
			return errMissingAction
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
}
