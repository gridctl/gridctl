package runtime

import (
	"fmt"
	"strings"
)

// DependencyGraph represents a directed acyclic graph of dependencies.
// It provides topological sorting to determine correct startup order.
type DependencyGraph struct {
	nodes    map[string]bool
	edges    map[string][]string // from -> []to (node depends on these)
	reversed map[string][]string // to -> []from (nodes that depend on this)
}

// NewDependencyGraph creates an empty dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:    make(map[string]bool),
		edges:    make(map[string][]string),
		reversed: make(map[string][]string),
	}
}

// AddNode adds a node to the graph.
func (g *DependencyGraph) AddNode(name string) {
	g.nodes[name] = true
}

// AddEdge adds a dependency edge: from depends on to.
// This means 'to' must be started before 'from'.
func (g *DependencyGraph) AddEdge(from, to string) {
	g.nodes[from] = true
	g.nodes[to] = true
	g.edges[from] = append(g.edges[from], to)
	g.reversed[to] = append(g.reversed[to], from)
}

// Sort returns nodes in topological order (dependencies first).
// Nodes without dependencies come first, dependent nodes come later.
// Uses Kahn's algorithm: O(V + E) time complexity.
func (g *DependencyGraph) Sort() ([]string, error) {
	// in-degree = number of dependencies a node has
	inDegree := make(map[string]int)
	for node := range g.nodes {
		inDegree[node] = len(g.edges[node])
	}

	// Start with nodes that have no dependencies (in-degree 0)
	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		// Pop from queue
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		// For each node that depends on this one, decrement its in-degree
		for _, dependent := range g.reversed[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles (if not all nodes were processed)
	if len(sorted) != len(g.nodes) {
		sortedSet := make(map[string]bool)
		for _, s := range sorted {
			sortedSet[s] = true
		}
		var remaining []string
		for node := range g.nodes {
			if !sortedSet[node] {
				remaining = append(remaining, node)
			}
		}
		return nil, fmt.Errorf("circular dependency detected involving: %s", strings.Join(remaining, ", "))
	}

	return sorted, nil
}

// GetDependencies returns the direct dependencies of a node.
func (g *DependencyGraph) GetDependencies(name string) []string {
	return g.edges[name]
}

// HasNode returns true if the node exists in the graph.
func (g *DependencyGraph) HasNode(name string) bool {
	return g.nodes[name]
}
