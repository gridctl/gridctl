package mcp

import (
	"context"
	"maps"
)

// A2AStatus contains only local trust state and aggregate authority counts.
// No routing identities, capability digests, or credentials are projected.
type A2AStatus struct {
	Dialect        string            `json:"dialect,omitempty"`
	CardTrust      string            `json:"cardTrust"`
	OmittedSkills  map[string]string `json:"omittedSkills,omitempty"`
	LiveRoots      int               `json:"liveRoots"`
	LiveTasks      int               `json:"liveTasks"`
	ExpiredRoots   int               `json:"expiredRoots"`
	UncertainRoots int               `json:"uncertainRoots"`
}

func (c *A2AClient) status() *A2AStatus {
	status := &A2AStatus{CardTrust: "unavailable"}
	if err := c.trust.lock(context.Background()); err != nil {
		return status
	}
	defer c.trust.unlock()
	e := c.trust.entries[c.name]
	if c.closed || e == nil || e.generation != c.registration || e.refetch == nil {
		return status
	}
	status.CardTrust = "approved"
	if e.blocked {
		status.CardTrust = "approval_required"
	}
	if c.published != nil {
		status.Dialect = c.published.version
		status.OmittedSkills = maps.Clone(c.published.tools.omitted)
	}
	if c.authority == nil {
		return status
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	now := c.store.now()
	for root := range c.authority.roots {
		if root.expired || !now.Before(root.expires) || !root.lease.IsZero() && !now.Before(root.lease) {
			status.ExpiredRoots++
			continue
		}
		status.LiveRoots++
		status.LiveTasks += len(root.tasks)
		uncertain := root.unknownWork
		for task := range root.tasks {
			uncertain = uncertain || task.uncertain
		}
		if uncertain {
			status.UncertainRoots++
		}
	}
	return status
}
