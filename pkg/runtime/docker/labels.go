package docker

// Labels used to identify gridctl-managed resources.
const (
	LabelManaged   = "gridctl.managed"
	LabelStack     = "gridctl.stack"
	LabelMCPServer = "gridctl.mcp-server"
	LabelResource  = "gridctl.resource"
	LabelAgent     = "gridctl.agent"
)

// ManagedLabels returns labels that identify a managed container.
func ManagedLabels(stack, name string, isMCPServer bool) map[string]string {
	labels := map[string]string{
		LabelManaged: "true",
		LabelStack:   stack,
	}
	if isMCPServer {
		labels[LabelMCPServer] = name
	} else {
		labels[LabelResource] = name
	}
	return labels
}

// AgentLabels returns labels that identify a managed agent container.
func AgentLabels(stack, name string) map[string]string {
	return map[string]string{
		LabelManaged: "true",
		LabelStack:   stack,
		LabelAgent:   name,
	}
}

// ContainerName generates a deterministic container name.
func ContainerName(stack, name string) string {
	return "gridctl-" + stack + "-" + name
}
