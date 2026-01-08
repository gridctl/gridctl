package runtime

// Labels used to identify agentlab-managed resources.
const (
	LabelManaged   = "agentlab.managed"
	LabelTopology  = "agentlab.topology"
	LabelMCPServer = "agentlab.mcp-server"
	LabelResource  = "agentlab.resource"
	LabelAgent     = "agentlab.agent"
)

// ManagedLabels returns labels that identify a managed container.
func ManagedLabels(topology, name string, isMCPServer bool) map[string]string {
	labels := map[string]string{
		LabelManaged:  "true",
		LabelTopology: topology,
	}
	if isMCPServer {
		labels[LabelMCPServer] = name
	} else {
		labels[LabelResource] = name
	}
	return labels
}

// AgentLabels returns labels that identify a managed agent container.
func AgentLabels(topology, name string) map[string]string {
	return map[string]string{
		LabelManaged:  "true",
		LabelTopology: topology,
		LabelAgent:    name,
	}
}

// ContainerName generates a deterministic container name.
func ContainerName(topology, name string) string {
	return "agentlab-" + topology + "-" + name
}
