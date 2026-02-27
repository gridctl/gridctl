package registry

import (
	"fmt"
	"strings"
)

// BuildWorkflowDAG constructs a dependency graph from workflow steps
// and returns steps in topological execution order, grouped by level.
// Steps within the same level have all dependencies satisfied and can run concurrently.
// Returns an error if cycles are detected or depends_on references are invalid.
func BuildWorkflowDAG(steps []WorkflowStep) ([][]WorkflowStep, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	// Index steps by ID
	stepByID := make(map[string]*WorkflowStep, len(steps))
	for i := range steps {
		stepByID[steps[i].ID] = &steps[i]
	}

	// Validate depends_on references and build adjacency
	// edges: step ID -> step IDs it depends on
	// reversed: step ID -> step IDs that depend on it
	edges := make(map[string][]string, len(steps))
	reversed := make(map[string][]string, len(steps))
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, ok := stepByID[dep]; !ok {
				suggestion := suggestStepID(dep, stepByID)
				if suggestion != "" {
					return nil, fmt.Errorf("step '%s' depends_on references unknown step '%s' (did you mean '%s'?)", step.ID, dep, suggestion)
				}
				return nil, fmt.Errorf("step '%s' depends_on references unknown step '%s'", step.ID, dep)
			}
			edges[step.ID] = append(edges[step.ID], dep)
			reversed[dep] = append(reversed[dep], step.ID)
		}
	}

	// Kahn's algorithm: compute in-degree (number of dependencies)
	inDegree := make(map[string]int, len(steps))
	for _, step := range steps {
		inDegree[step.ID] = len(edges[step.ID])
	}

	// Start with steps that have no dependencies
	var queue []string
	for _, step := range steps {
		if inDegree[step.ID] == 0 {
			queue = append(queue, step.ID)
		}
	}

	// Process level by level
	var levels [][]WorkflowStep
	var processed int
	for len(queue) > 0 {
		// All items currently in the queue form one level
		level := make([]WorkflowStep, 0, len(queue))
		var nextQueue []string
		for _, id := range queue {
			level = append(level, *stepByID[id])
			processed++

			// Decrement in-degree for dependents
			for _, dependent := range reversed[id] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					nextQueue = append(nextQueue, dependent)
				}
			}
		}
		levels = append(levels, level)
		queue = nextQueue
	}

	// Check for cycles
	if processed != len(steps) {
		cycle := findCycle(edges, inDegree)
		return nil, fmt.Errorf("cycle detected: %s", cycle)
	}

	return levels, nil
}

// findCycle traces a cycle in the graph for a descriptive error message.
func findCycle(edges map[string][]string, inDegree map[string]int) string {
	// Find a node still in the cycle (in-degree > 0)
	var start string
	for id, deg := range inDegree {
		if deg > 0 {
			start = id
			break
		}
	}

	// Follow edges to trace the cycle
	visited := make(map[string]bool)
	var path []string
	current := start
	for !visited[current] {
		visited[current] = true
		path = append(path, current)
		for _, dep := range edges[current] {
			if inDegree[dep] > 0 {
				current = dep
				break
			}
		}
	}

	// Find where the cycle starts in the path
	cycleStart := current
	var cycle []string
	recording := false
	for _, id := range path {
		if id == cycleStart {
			recording = true
		}
		if recording {
			cycle = append(cycle, id)
		}
	}
	cycle = append(cycle, cycleStart) // close the cycle

	// Format as "step 'A' depends on 'B' which depends on 'A'"
	var parts []string
	for i := 0; i < len(cycle)-1; i++ {
		parts = append(parts, fmt.Sprintf("'%s'", cycle[i]))
	}
	return "step " + strings.Join(parts, " depends on ") + fmt.Sprintf(" which depends on '%s'", cycle[len(cycle)-1])
}

// suggestStepID finds a close match for a misspelled step ID.
func suggestStepID(target string, stepByID map[string]*WorkflowStep) string {
	var best string
	bestDist := len(target)/2 + 1 // max edit distance threshold
	for id := range stepByID {
		d := levenshtein(target, id)
		if d < bestDist {
			bestDist = d
			best = id
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
