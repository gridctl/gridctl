package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnifiedDiff_Identical(t *testing.T) {
	assert.Equal(t, "", unifiedDiff("a\nb\n", "a\nb\n", 3))
	assert.Equal(t, "", unifiedDiff("", "", 3))
}

func TestUnifiedDiff_AddedLines(t *testing.T) {
	old := "version: \"1\"\nname: example\n"
	updated := "version: \"1\"\nname: example\nclients:\n  default: deny\n"
	diff := unifiedDiff(old, updated, 3)

	assert.Contains(t, diff, "+clients:")
	assert.Contains(t, diff, "+  default: deny")
	// Unchanged context lines carry a single leading space, not a +/-.
	assert.Contains(t, diff, " version: \"1\"")
	assert.NotContains(t, diff, "-version")
	assert.True(t, strings.HasPrefix(diff, "@@"), "diff should start with a hunk header")
}

func TestUnifiedDiff_RemovedAndChanged(t *testing.T) {
	old := "a\nb\nc\n"
	updated := "a\nB\nc\n"
	diff := unifiedDiff(old, updated, 1)
	assert.Contains(t, diff, "-b")
	assert.Contains(t, diff, "+B")
	assert.Contains(t, diff, " a")
	assert.Contains(t, diff, " c")
}

func TestUnifiedDiff_ContextTrimsDistantLines(t *testing.T) {
	// 20 identical lines with one change in the middle; context=2 should elide
	// the far-away unchanged lines into separate/limited context.
	var oldB, newB strings.Builder
	for i := 0; i < 20; i++ {
		oldB.WriteString("line\n")
		if i == 10 {
			newB.WriteString("CHANGED\n")
		} else {
			newB.WriteString("line\n")
		}
	}
	diff := unifiedDiff(oldB.String(), newB.String(), 2)
	assert.Contains(t, diff, "+CHANGED")
	assert.Contains(t, diff, "-line")
	// Only a handful of context lines survive, not all 19 unchanged ones.
	assert.LessOrEqual(t, strings.Count(diff, " line"), 5)
}

func TestUnifiedDiff_HunkHeaderCounts(t *testing.T) {
	diff := unifiedDiff("a\n", "a\nb\n", 3)
	// One context line (a) + one added (b): old has 1 line, new has 2.
	assert.Contains(t, diff, "@@ -1,1 +1,2 @@")
}
