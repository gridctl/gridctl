package output

import "strings"

// suggestMaxDistance is the largest Levenshtein distance still considered
// a plausible typo, matching cobra's default suggestion distance.
const suggestMaxDistance = 2

// Suggest returns the candidate closest to input within a Levenshtein
// distance of suggestMaxDistance, or "" when nothing is close enough.
// Matching is case-insensitive; ties resolve to the earliest candidate.
func Suggest(input string, candidates []string) string {
	best := ""
	bestDist := suggestMaxDistance + 1
	lowered := strings.ToLower(input)
	for _, c := range candidates {
		d := levenshtein(lowered, strings.ToLower(c))
		if d < bestDist {
			best = c
			bestDist = d
		}
	}
	if bestDist > suggestMaxDistance {
		return ""
	}
	return best
}

// levenshtein computes the edit distance between two strings by rune.
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func minInt(nums ...int) int {
	m := nums[0]
	for _, n := range nums[1:] {
		if n < m {
			m = n
		}
	}
	return m
}
