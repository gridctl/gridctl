package config

import "reflect"

// SourceEqual reports whether two source declarations have the same effective configuration.
func SourceEqual(a, b *Source) bool {
	return reflect.DeepEqual(canonicalSource(a), canonicalSource(b))
}

// MCPServerEqual reports whether two MCP server declarations have the same effective configuration.
func MCPServerEqual(a, b MCPServer) bool {
	canonicalizeMCPServer(&a)
	canonicalizeMCPServer(&b)
	return reflect.DeepEqual(a, b)
}

func canonicalizeMCPServer(server *MCPServer) {
	if server.Source != nil {
		server.Source = canonicalSource(server.Source)
	}
	if server.Autoscale == nil && server.Replicas <= 0 {
		server.Replicas = 1
	}
	if server.ReplicaPolicy == "" {
		server.ReplicaPolicy = "round-robin"
	}
	if len(server.Command) == 0 {
		server.Command = nil
	}
	if len(server.Env) == 0 {
		server.Env = nil
	}
	if len(server.BuildArgs) == 0 {
		server.BuildArgs = nil
	}
	if len(server.Tools) == 0 {
		server.Tools = nil
	}
	if len(server.Volumes) == 0 {
		server.Volumes = nil
	}
	if server.Auth != nil && len(server.Auth.Scopes) == 0 {
		copy := *server.Auth
		copy.Scopes = nil
		server.Auth = &copy
	}
	if server.OpenAPI != nil && server.OpenAPI.Operations != nil {
		openAPI := *server.OpenAPI
		operations := *openAPI.Operations
		if len(operations.Include) == 0 {
			operations.Include = nil
		}
		if len(operations.Exclude) == 0 {
			operations.Exclude = nil
		}
		openAPI.Operations = &operations
		server.OpenAPI = &openAPI
	}
	if server.Autoscale != nil {
		autoscale := *server.Autoscale
		autoscale.ScaleUpAfter = autoscale.ResolvedScaleUpAfter().String()
		autoscale.ScaleDownAfter = autoscale.ResolvedScaleDownAfter().String()
		server.Autoscale = &autoscale
	}
}

func canonicalSource(source *Source) *Source {
	if source == nil {
		return nil
	}
	copy := *source
	if copy.Dockerfile == "" {
		copy.Dockerfile = "Dockerfile"
	}
	if copy.Type == "git" && copy.Ref == "" {
		copy.Ref = "main"
	}
	return &copy
}
