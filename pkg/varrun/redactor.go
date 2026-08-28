package varrun

import (
	"bytes"
	"io"
	"sort"
	"sync"
)

var replacement = []byte("[REDACTED]")

// NewRedactor returns a bounded streaming writer that replaces exact secret
// byte strings, including matches split across Write calls.
func NewRedactor(dst io.Writer, values []string) io.WriteCloser {
	seen := make(map[string]bool)
	secrets := make([][]byte, 0, len(values))
	max := 0
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		b := []byte(value)
		secrets = append(secrets, b)
		if len(b) > max {
			max = len(b)
		}
	}
	sort.Slice(secrets, func(i, j int) bool {
		if len(secrets[i]) == len(secrets[j]) {
			return bytes.Compare(secrets[i], secrets[j]) < 0
		}
		return len(secrets[i]) > len(secrets[j])
	})
	return &redactor{dst: dst, secrets: secrets, max: max}
}

type redactor struct {
	mu      sync.Mutex
	dst     io.Writer
	secrets [][]byte
	max     int
	buf     []byte
	err     error
}

func (r *redactor) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return 0, r.err
	}
	r.buf = append(r.buf, p...)
	keep := r.max - 1
	if keep < 0 {
		keep = 0
	}
	r.process(len(r.buf)-keep, false)
	if r.err != nil {
		return 0, r.err
	}
	return len(p), nil
}

func (r *redactor) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.process(len(r.buf), true)
	return r.err
}

func (r *redactor) process(limit int, final bool) {
	if r.err != nil || limit <= 0 {
		return
	}
	if len(r.secrets) == 0 {
		r.write(r.buf[:limit])
		r.buf = r.buf[limit:]
		return
	}
	i := 0
	for i < limit {
		var match []byte
		for _, secret := range r.secrets {
			if len(r.buf)-i >= len(secret) && bytes.Equal(r.buf[i:i+len(secret)], secret) {
				match = secret
				break
			}
		}
		if match != nil {
			r.write(replacement)
			i += len(match)
			continue
		}
		if !final && len(r.buf)-i < r.max {
			break
		}
		r.write(r.buf[i : i+1])
		i++
	}
	r.buf = append(r.buf[:0], r.buf[i:]...)
}

func (r *redactor) write(p []byte) {
	if r.err != nil || len(p) == 0 {
		return
	}
	var n int
	n, r.err = r.dst.Write(p)
	if r.err == nil && n != len(p) {
		r.err = io.ErrShortWrite
	}
}
