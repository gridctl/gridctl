package tracing

import (
	"crypto/rand"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
)

const fallbackServiceName = "gridctl-gateway"

// ResourceIdentity is the three resource attributes shared by the trace and
// meter providers. InstanceID is generated once per provider instance.
type ResourceIdentity struct {
	ServiceName string
	Version     string
	InstanceID  string
}

// NewResourceIdentity fills an empty service name and instance id.
func NewResourceIdentity(serviceName, version, instanceID string) ResourceIdentity {
	if serviceName == "" {
		serviceName = fallbackServiceName
	}
	if instanceID == "" {
		instanceID = newInstanceID()
	}
	return ResourceIdentity{ServiceName: serviceName, Version: version, InstanceID: instanceID}
}

// BuildResource returns the shared resource. The schema URL is left empty:
// these are the stable service.* keys, not a pinned semconv schema.
func BuildResource(id ResourceIdentity) *sdkresource.Resource {
	id = NewResourceIdentity(id.ServiceName, id.Version, id.InstanceID)
	return sdkresource.NewWithAttributes(
		"",
		attribute.String(string(AttrServiceName), id.ServiceName),
		attribute.String(string(AttrServiceVersion), id.Version),
		attribute.String(string(AttrServiceInstanceID), id.InstanceID),
	)
}

func newInstanceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "gridctl-instance"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
