package registry

// Skill execution for AgentSkill is handled differently than the old Step-based
// tool chain model. AgentSkill bodies are markdown instructions returned as content
// via CallTool in server.go. The old step-based executor is removed.
//
// This file is retained as a placeholder for future execution capabilities
// (e.g., running scripts from the skills/ directory).
