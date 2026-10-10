package runtime

import "github.com/distribution/reference"

// ImageReferenceMatches reports whether two image names are the same reference.
// Engines may expand Docker Hub names and default tags during creation.
// Different registries, tags, and digests do not match. An identical non-empty
// string matches even when it is not a reference. Two empty names do not match.
func ImageReferenceMatches(current, requested string) bool {
	if current != "" && current == requested {
		return true
	}
	a, err := reference.ParseNormalizedNamed(current)
	if err != nil {
		return false
	}
	b, err := reference.ParseNormalizedNamed(requested)
	return err == nil && reference.TagNameOnly(a).String() == reference.TagNameOnly(b).String()
}
