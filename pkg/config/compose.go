package config

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// fragmentAllowedKeys are the only top-level keys a stack fragment may carry.
// Gateway, network, and every other stack block stay with the base pack.
var fragmentAllowedKeys = map[string]bool{
	"version":     true,
	"mcp-servers": true,
	"resources":   true,
	"variables":   true,
}

// Layer is one input to Compose. Dir is the checkout directory and Stack is
// the decoded document; neither is safe to encode (a plan DTO carries pack,
// commit, and file only).
type Layer struct {
	Pack   string `json:"pack"`
	Commit string `json:"commit"`
	Path   string `json:"file"`
	Dir    string `json:"-"`
	Stack  *Stack `json:"-"`
}

// ReadUnresolved reads a stack the way ExportStack does, without discovering
// or merging an override and without expanding variables.
func ReadUnresolved(ctx context.Context, path string) (*Stack, error) {
	var sources []string
	stack, err := readExportStack(ctx, path, &sources)
	if err != nil {
		return nil, err
	}
	if err := checkExportCredentials(stack); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return stack, nil
}

// ValidateFragment rejects disallowed top-level keys, then decodes through
// the non-resolving export reader and checks export credentials. A fragment
// with no servers and no resources is an error. The path is included in
// error text; callers that want a repo-relative label should pass that label
// only when it is also the file to read, or rewrite the path in the message.
func ValidateFragment(path string) (*Stack, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- caller-selected fragment path
	if err != nil {
		return nil, fmt.Errorf("fragment %s: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("fragment %s: %w", path, err)
	}
	var rejected []string
	for key := range raw {
		if fragmentAllowedKeys[key] {
			continue
		}
		rejected = append(rejected, key)
	}
	if len(rejected) > 0 {
		slices.Sort(rejected)
		return nil, fmt.Errorf("fragment %s: '%s' is not allowed in a stack fragment; fragments may declare mcp-servers, resources, and variables only", path, rejected[0])
	}
	stack, err := ReadUnresolved(context.Background(), path)
	if err != nil {
		return nil, fmt.Errorf("fragment %s: %w", path, err)
	}
	if stack == nil || (len(stack.MCPServers) == 0 && len(stack.Resources) == 0) {
		return nil, fmt.Errorf("fragment %s: a fragment must declare at least one mcp-server or resource", path)
	}
	return stack, nil
}

// Compose copies base and appends each fragment's servers, resources, and
// variables. Relative paths are rewritten against the layer checkout before
// the merge, because the composed file does not live beside any layer.
// A name already present is an error naming both owners. Pure: no stores,
// no expansion, no I/O.
func Compose(base Layer, fragments []Layer) (*Stack, error) {
	if base.Stack == nil {
		return nil, fmt.Errorf("base pack %q has no stack", base.Pack)
	}
	out, err := cloneStack(base.Stack)
	if err != nil {
		return nil, err
	}
	resolveRelativePaths(out, layerDir(base))
	serverOwner := map[string]string{}
	resourceOwner := map[string]string{}
	for _, srv := range out.MCPServers {
		serverOwner[srv.Name] = base.Pack
	}
	for _, res := range out.Resources {
		resourceOwner[res.Name] = base.Pack
	}
	for _, frag := range fragments {
		if frag.Stack == nil {
			return nil, fmt.Errorf("fragment pack %q has no stack", frag.Pack)
		}
		layer, err := cloneStack(frag.Stack)
		if err != nil {
			return nil, err
		}
		resolveRelativePaths(layer, layerDir(frag))
		for _, srv := range layer.MCPServers {
			if prev, ok := serverOwner[srv.Name]; ok {
				return nil, fmt.Errorf("server '%s' is declared by pack '%s' and pack '%s'", srv.Name, prev, frag.Pack)
			}
			serverOwner[srv.Name] = frag.Pack
			out.MCPServers = append(out.MCPServers, srv)
		}
		for _, res := range layer.Resources {
			if prev, ok := resourceOwner[res.Name]; ok {
				return nil, fmt.Errorf("resource '%s' is declared by pack '%s' and pack '%s'", res.Name, prev, frag.Pack)
			}
			resourceOwner[res.Name] = frag.Pack
			out.Resources = append(out.Resources, res)
		}
		if err := checkDeclarationConflicts(layer.Variables, out.Variables); err != nil {
			return nil, fmt.Errorf("pack %q: %w", frag.Pack, err)
		}
		mergeVariableDeclarations(out, layer)
	}
	return out, nil
}

// layerDir is the directory relative paths in that layer are resolved
// against: the checkout joined with the stack file's parent. A file at the
// checkout root resolves against Dir.
func layerDir(l Layer) string {
	if l.Dir == "" {
		return ""
	}
	if l.Path == "" {
		return l.Dir
	}
	return filepath.Join(l.Dir, filepath.Dir(filepath.FromSlash(l.Path)))
}

func cloneStack(s *Stack) (*Stack, error) {
	data, err := yaml.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("copying stack: %w", err)
	}
	return DecodeStack(data)
}

// WriteComposed writes a provenance header and the marshaled stack. The
// marshal path is yaml.Marshal, the same one gridctl export uses, so explicit
// false values survive. The write is atomic. layers[0] is the base.
func WriteComposed(path string, stack *Stack, layers []Layer) error {
	data, err := RenderComposed(stack, layers)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating composed stack directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { // #nosec G306 -- composed stack is not a credential
		_ = os.Remove(tmp)
		return fmt.Errorf("writing composed stack: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming composed stack: %w", err)
	}
	return nil
}

// ReadComposedHeader parses a composed-stack header. ok is false when the
// file has no composed header. Dir and Stack are left empty.
func ReadComposedHeader(path string) ([]Layer, bool, error) {
	f, err := os.Open(path) // #nosec G304 -- caller-selected composed path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return nil, false, sc.Err()
	}
	if !strings.HasPrefix(sc.Text(), "# gridctl composed stack.") {
		return nil, false, nil
	}
	var layers []Layer
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "# layer: ") {
			break
		}
		layer, err := parseLayerLine(line)
		if err != nil {
			return nil, false, err
		}
		layers = append(layers, layer)
	}
	if err := sc.Err(); err != nil {
		return nil, false, err
	}
	if len(layers) == 0 {
		return nil, false, fmt.Errorf("composed stack %s has a header but no layers", path)
	}
	return layers, true, nil
}

func parseLayerLine(line string) (Layer, error) {
	fields := strings.Fields(strings.TrimPrefix(line, "# layer: "))
	if len(fields) < 4 {
		return Layer{}, fmt.Errorf("composed layer line %q is incomplete", line)
	}
	kind := fields[0]
	if kind != "base" && kind != "fragment" {
		return Layer{}, fmt.Errorf("composed layer line %q has unknown kind", line)
	}
	var layer Layer
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok || value == "" {
			return Layer{}, fmt.Errorf("composed layer line %q has a bad field", line)
		}
		switch key {
		case "pack":
			layer.Pack = value
		case "commit":
			layer.Commit = value
		case "file":
			layer.Path = value
		default:
			return Layer{}, fmt.Errorf("composed layer line %q has unknown field %q", line, key)
		}
	}
	if layer.Pack == "" || layer.Commit == "" || layer.Path == "" {
		return Layer{}, fmt.Errorf("composed layer line %q is missing pack, commit, or file", line)
	}
	return layer, nil
}

// RenderComposed returns the bytes WriteComposed would write, without I/O.
func RenderComposed(stack *Stack, layers []Layer) ([]byte, error) {
	if stack == nil {
		return nil, fmt.Errorf("composed stack is nil")
	}
	body, err := yaml.Marshal(stack)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	base := ""
	if len(layers) > 0 {
		base = layers[0].Pack
	}
	fmt.Fprintf(&buf, "# gridctl composed stack. Do not edit; re-run 'gridctl pack apply %s'.\n", base)
	for i, layer := range layers {
		kind := "fragment"
		if i == 0 {
			kind = "base"
		}
		fmt.Fprintf(&buf, "# layer: %s pack=%s commit=%s file=%s\n", kind, layer.Pack, layer.Commit, layer.Path)
	}
	buf.Write(body)
	return buf.Bytes(), nil
}
