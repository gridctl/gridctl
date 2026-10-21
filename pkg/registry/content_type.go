package registry

import "path/filepath"

// ContentTypeForPath uses a fixed MIME table independent of host configuration.
func ContentTypeForPath(path string) string {
	switch filepath.Ext(path) {
	case ".md":
		return "text/markdown"
	case ".sh":
		return "text/x-shellscript"
	case ".py":
		return "text/x-python"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".csv":
		return "text/csv"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".txt":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "text/javascript"
	case ".toml":
		return "application/toml"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
