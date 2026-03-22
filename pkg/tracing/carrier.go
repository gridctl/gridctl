package tracing

// MetaCarrier implements propagation.TextMapCarrier for MCP _meta maps.
// It enables standard OTel Extract()/Inject() calls on the JSON-RPC
// params._meta field, following MCP spec PR #414.
type MetaCarrier struct {
	meta map[string]any
}

// NewMetaCarrier wraps an existing _meta map (or creates one if nil).
func NewMetaCarrier(meta map[string]any) *MetaCarrier {
	if meta == nil {
		meta = make(map[string]any)
	}
	return &MetaCarrier{meta: meta}
}

// Get returns the value for a key, normalising to lowercase.
func (c *MetaCarrier) Get(key string) string {
	if v, ok := c.meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Set stores a key-value pair in the _meta map.
func (c *MetaCarrier) Set(key, value string) {
	c.meta[key] = value
}

// Keys returns all keys present in the _meta map.
func (c *MetaCarrier) Keys() []string {
	keys := make([]string, 0, len(c.meta))
	for k := range c.meta {
		keys = append(keys, k)
	}
	return keys
}

// Map returns the underlying _meta map (e.g. to re-marshal into JSON).
func (c *MetaCarrier) Map() map[string]any {
	return c.meta
}
