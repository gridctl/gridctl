package config

import (
	"bytes"
	"encoding/gob"

	"gopkg.in/yaml.v3"
)

// Clone returns a deep copy. Unexported fields stay intact because the copy
// runs in this package. A nil stack returns nil.
func (s *Stack) Clone() *Stack {
	if s == nil {
		return nil
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(s); err != nil {
		return cloneStackFallback(s)
	}
	var out Stack
	if err := gob.NewDecoder(&buf).Decode(&out); err != nil {
		return cloneStackFallback(s)
	}
	return &out
}

// cloneStackFallback copies exported fields through the YAML codec when gob
// cannot encode a value. Computed indexes are copied by hand. scopeDeclared
// on set refs is not preserved; resolution runs after set injection.
func cloneStackFallback(s *Stack) *Stack {
	data, err := yaml.Marshal(s)
	if err != nil {
		return nil
	}
	out, err := DecodeStack(data)
	if err != nil || out == nil {
		return nil
	}
	out.References = cloneReferenceIndex(s.References)
	out.SecretRefs = append([]SecretRef(nil), s.SecretRefs...)
	out.UnresolvedRefs = append([]string(nil), s.UnresolvedRefs...)
	out.Extends = s.Extends
	out.Overrides = s.Overrides
	out.OverridePath = s.OverridePath
	return out
}

func cloneReferenceIndex(in ReferenceIndex) ReferenceIndex {
	if in == nil {
		return nil
	}
	out := make(ReferenceIndex, len(in))
	for key, consumers := range in {
		out[key] = append([]Consumer(nil), consumers...)
	}
	return out
}
