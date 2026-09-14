package stackpolicy

import (
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"gopkg.in/yaml.v3"
)

type serverKind int

const (
	kindUnknown serverKind = iota
	kindMalformed
	kindImage
	kindSource
	kindURL
	kindLocal
	kindSSH
	kindOpenAPI
)

var knownServerKeys = map[string]struct{}{
	"execution": {}, "name": {}, "image": {}, "source": {}, "url": {}, "port": {},
	"transport": {}, "command": {}, "env": {}, "build_args": {}, "volumes": {},
	"network": {}, "ssh": {}, "openapi": {}, "tools": {}, "output_format": {},
	"pin_schemas": {}, "ready_timeout": {}, "ping_timeout": {}, "protocol_generation": {},
	"replicas": {}, "replica_policy": {}, "autoscale": {}, "telemetry": {}, "auth": {},
}

var knownPinningKeys = map[string]struct{}{
	"enabled": {}, "action": {}, "scan": {}, "scan_ignore": {},
}

var securityAliasKeys = map[string]struct{}{
	"pin-schemas": {}, "schema_pinning": {}, "schema-pinning": {}, "pinning": {},
	"privileged": {}, "privilege": {}, "security": {},
}

type declaredStack struct {
	membershipUnknown bool
	pinningUnknown    bool
	globalEnabled     *bool
	globalEnabledUnk  bool
	globalAction      string
	globalActionUnk   bool
	servers           []mcpSubject
	resources         []resourceSubject
}

type mcpSubject struct {
	loc            Location
	index          int
	kind           serverKind
	kindUnknown    bool
	image          string
	imagePresent   bool
	imageDynamic   bool
	imageInvalid   bool
	sourcePresent  bool
	sourceDynamic  bool
	sourceType     string
	toolsPresent   bool
	toolsDynamic   bool
	toolsInvalid   bool
	tools          []toolItem
	pinSchemas     *bool
	pinUnknown     bool
}

type toolItem struct {
	value   string
	dynamic bool
	empty   bool
	loc     Location
}

type resourceSubject struct {
	loc          Location
	index        int
	nameDynamic  bool
	image        string
	imagePresent bool
	imageDynamic bool
	imageInvalid bool
}

type fileDecl struct {
	alias    string
	gateway  *yaml.Node
	servers  []*yaml.Node
	resources []*yaml.Node
	serverNames []nameField
	resourceNames []nameField
}

type nameField struct {
	value   string
	dynamic bool
}

func interpretSnapshot(snap *snapshot) (*declaredStack, []Diagnostic) {
	var diags []Diagnostic
	if snap == nil || len(snap.files) == 0 {
		return nil, []Diagnostic{{Code: "input-unreadable", Location: Location{Source: "entry"}, Message: diagnosticMessage("input-unreadable")}}
	}
	decls := make([]fileDecl, 0, len(snap.files))
	for _, f := range snap.files {
		d, ds := interpretFile(f)
		diags = append(diags, ds...)
		decls = append(decls, d)
	}
	if len(diags) > 0 {
		return nil, diags
	}
	out := mergeDecls(decls)
	return out, nil
}

func interpretFile(f capturedFile) (fileDecl, []Diagnostic) {
	var diags []Diagnostic
	doc, err := decodeSingleDocument(f.bytes)
	if err != nil {
		return fileDecl{}, []Diagnostic{{Code: errorCode(err), Location: Location{Source: f.alias}, Message: diagnosticMessage(errorCode(err))}}
	}
	root, err := documentMapping(doc)
	if err != nil {
		return fileDecl{}, []Diagnostic{{Code: "input-invalid", Location: Location{Source: f.alias}, Message: diagnosticMessage("input-invalid")}}
	}
	d := fileDecl{alias: f.alias}
	if g := mappingValue(root, "gateway"); g != nil && !isNull(g) {
		d.gateway = g
	}
	if n := mappingValue(root, "mcp-servers"); n != nil && !isNull(n) {
		if n.Kind != yaml.SequenceNode {
			if nodeDynamic(n) {
				d.serverNames = []nameField{{dynamic: true}}
			} else {
				diags = append(diags, Diagnostic{Code: "input-invalid", Location: locOf(n, f.alias, "mcp-servers"), Message: diagnosticMessage("input-invalid")})
			}
		} else {
			for i, item := range n.Content {
				node := resolveAlias(item)
				d.servers = append(d.servers, node)
				d.serverNames = append(d.serverNames, mappingName(node))
				_ = i
			}
		}
	}
	if n := mappingValue(root, "resources"); n != nil && !isNull(n) {
		if n.Kind != yaml.SequenceNode {
			if nodeDynamic(n) {
				d.resourceNames = []nameField{{dynamic: true}}
			} else {
				diags = append(diags, Diagnostic{Code: "input-invalid", Location: locOf(n, f.alias, "resources"), Message: diagnosticMessage("input-invalid")})
			}
		} else {
			for _, item := range n.Content {
				node := resolveAlias(item)
				d.resources = append(d.resources, node)
				d.resourceNames = append(d.resourceNames, mappingName(node))
			}
		}
	}
	return d, diags
}

func mappingName(n *yaml.Node) nameField {
	if n == nil || n.Kind != yaml.MappingNode {
		return nameField{dynamic: nodeDynamic(n)}
	}
	v := mappingValue(n, "name")
	if v == nil || isNull(v) {
		return nameField{}
	}
	if v.Kind != yaml.ScalarNode {
		return nameField{dynamic: nodeDynamic(v)}
	}
	return nameField{value: v.Value, dynamic: config.ContainsExpansion(v.Value)}
}

type mappedItem struct {
	alias string
	node  *yaml.Node
}

func mergeDecls(decls []fileDecl) *declaredStack {
	out := &declaredStack{}
	if namesUncertain(decls, true) {
		out.membershipUnknown = true
	}
	for _, d := range decls {
		if d.gateway != nil {
			applyGateway(out, d.gateway, d.alias)
			break
		}
	}
	if out.membershipUnknown {
		return out
	}
	for i, item := range mergeNamed(decls, func(d fileDecl) []*yaml.Node { return d.servers }) {
		out.servers = append(out.servers, interpretServer(item.node, item.alias, i))
	}
	for i, item := range mergeNamed(decls, func(d fileDecl) []*yaml.Node { return d.resources }) {
		out.resources = append(out.resources, interpretResource(item.node, item.alias, i))
	}
	return out
}

func mergeNamed(decls []fileDecl, get func(fileDecl) []*yaml.Node) []mappedItem {
	seen := map[string]struct{}{}
	var items []mappedItem
	anon := 0
	for _, d := range decls {
		for _, node := range get(d) {
			nf := mappingName(node)
			key := nf.value
			if key == "" {
				key = d.alias + "#" + itoa(anon)
				anon++
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, mappedItem{alias: d.alias, node: node})
		}
	}
	return items
}

func namesUncertain(decls []fileDecl, _ bool) bool {
	for _, d := range decls {
		for _, n := range d.serverNames {
			if n.dynamic {
				return true
			}
		}
		for _, n := range d.resourceNames {
			if n.dynamic {
				return true
			}
		}
	}
	return false
}

func applyGateway(out *declaredStack, gateway *yaml.Node, alias string) {
	if gateway.Kind != yaml.MappingNode {
		if nodeDynamic(gateway) {
			out.pinningUnknown = true
			out.globalEnabledUnk = true
			out.globalActionUnk = true
		}
		return
	}
	sec := mappingValue(gateway, "security")
	if sec == nil || isNull(sec) {
		return
	}
	if sec.Kind != yaml.MappingNode {
		out.pinningUnknown = true
		return
	}
	for _, key := range mappingKeys(sec) {
		if key != "schema_pinning" {
			out.pinningUnknown = true
		}
	}
	pin := mappingValue(sec, "schema_pinning")
	if pin == nil || isNull(pin) {
		return
	}
	if pin.Kind != yaml.MappingNode {
		if nodeDynamic(pin) {
			out.pinningUnknown = true
			out.globalEnabledUnk = true
			out.globalActionUnk = true
		} else {
			out.pinningUnknown = true
		}
		return
	}
	for _, key := range mappingKeys(pin) {
		if _, ok := knownPinningKeys[key]; !ok {
			out.pinningUnknown = true
		}
	}
	if en := mappingValue(pin, "enabled"); en != nil && !isNull(en) {
		if nodeDynamic(en) {
			out.globalEnabledUnk = true
		} else if v, ok := scalarBool(en); ok {
			out.globalEnabled = &v
		} else {
			out.globalEnabledUnk = true
		}
	}
	if act := mappingValue(pin, "action"); act != nil && !isNull(act) {
		if nodeDynamic(act) {
			out.globalActionUnk = true
		} else if s, ok := scalarString(act); ok {
			switch s {
			case "", "warn", "block":
				out.globalAction = s
			default:
				out.globalAction = s
			}
		} else {
			out.globalActionUnk = true
		}
	}
	_ = alias
}

func interpretServer(node *yaml.Node, alias string, index int) mcpSubject {
	path := "mcp-servers[" + itoa(index) + "]"
	subj := mcpSubject{loc: locOf(node, alias, path), index: index}
	if node == nil || node.Kind != yaml.MappingNode {
		subj.kind = kindMalformed
		subj.kindUnknown = nodeDynamic(node)
		return subj
	}
	for _, key := range mappingKeys(node) {
		if _, ok := knownServerKeys[key]; ok {
			continue
		}
		if _, ok := securityAliasKeys[key]; ok {
			subj.pinUnknown = true
			subj.kindUnknown = true
		}
	}
	imageNode := mappingValue(node, "image")
	if imageNode != nil && !isNull(imageNode) {
		subj.imagePresent = true
		if nodeDynamic(imageNode) {
			subj.imageDynamic = true
		}
		if s, ok := scalarString(imageNode); ok {
			subj.image = s
			if s == "" {
				subj.imagePresent = false
			}
		} else {
			subj.imageInvalid = true
		}
	}
	sourceNode := mappingValue(node, "source")
	if sourceNode != nil && !isNull(sourceNode) {
		subj.sourcePresent = true
		if sourceNode.Kind != yaml.MappingNode {
			subj.sourceDynamic = nodeDynamic(sourceNode)
			if !subj.sourceDynamic {
				subj.kind = kindMalformed
			} else {
				subj.kindUnknown = true
			}
		} else {
			if t := mappingValue(sourceNode, "type"); t != nil && !isNull(t) {
				if nodeDynamic(t) {
					subj.sourceDynamic = true
					subj.kindUnknown = true
				} else if s, ok := scalarString(t); ok {
					subj.sourceType = s
				} else {
					subj.kindUnknown = true
				}
			}
		}
	}
	urlPresent := false
	if u := mappingValue(node, "url"); u != nil && !isNull(u) {
		if s, ok := scalarString(u); ok && s != "" {
			urlPresent = true
		} else if nodeDynamic(u) {
			urlPresent = true
			subj.kindUnknown = true
		} else if !isNull(u) {
			urlPresent = true
		}
	}
	cmdNode := mappingValue(node, "command")
	cmdPresent := commandPresent(cmdNode)
	sshNode := mappingValue(node, "ssh")
	sshPresent := sshNode != nil && !isNull(sshNode)
	openAPINode := mappingValue(node, "openapi")
	openAPIPresent := openAPINode != nil && !isNull(openAPINode)

	hasImage := subj.imagePresent
	hasSource := subj.sourcePresent
	hasURL := urlPresent
	hasSSH := sshPresent && cmdPresent
	hasCommand := cmdPresent && !hasImage && !hasSource && !hasURL && !hasSSH
	hasOpenAPI := openAPIPresent

	count := 0
	if hasImage {
		count++
	}
	if hasSource {
		count++
	}
	if hasURL {
		count++
	}
	if hasCommand {
		count++
	}
	if hasSSH {
		count++
	}
	if hasOpenAPI {
		count++
	}
	switch {
	case subj.kind == kindMalformed:
	case subj.kindUnknown && count <= 1:
	case count != 1:
		subj.kind = kindMalformed
	case hasImage:
		subj.kind = kindImage
	case hasSource:
		subj.kind = kindSource
	case hasURL:
		subj.kind = kindURL
	case hasCommand:
		subj.kind = kindLocal
	case hasSSH:
		subj.kind = kindSSH
	case hasOpenAPI:
		subj.kind = kindOpenAPI
	}

	if pin := mappingValue(node, "pin_schemas"); pin != nil && !isNull(pin) {
		if nodeDynamic(pin) {
			subj.pinUnknown = true
		} else if v, ok := scalarBool(pin); ok {
			subj.pinSchemas = &v
		} else {
			subj.pinUnknown = true
		}
	}

	toolsNode := mappingValue(node, "tools")
	if mappingHasKey(node, "tools") {
		subj.toolsPresent = true
		if toolsNode == nil || isNull(toolsNode) {
			subj.toolsInvalid = false
			subj.tools = nil
		} else if nodeDynamic(toolsNode) && toolsNode.Kind != yaml.SequenceNode {
			subj.toolsDynamic = true
		} else if toolsNode.Kind != yaml.SequenceNode {
			subj.toolsInvalid = true
		} else {
			for ti, item := range toolsNode.Content {
				n := resolveAlias(item)
				itemLoc := locOf(n, alias, path+".tools["+itoa(ti)+"]")
				if n.Kind != yaml.ScalarNode {
					if nodeDynamic(n) {
						subj.toolsDynamic = true
						subj.tools = append(subj.tools, toolItem{dynamic: true, loc: itemLoc})
					} else {
						subj.toolsInvalid = true
					}
					continue
				}
				val := n.Value
				item := toolItem{value: val, loc: itemLoc, empty: strings.TrimSpace(val) == "", dynamic: config.ContainsExpansion(val)}
				if item.dynamic {
					subj.toolsDynamic = true
				}
				subj.tools = append(subj.tools, item)
			}
		}
	}
	return subj
}

func commandPresent(n *yaml.Node) bool {
	if n == nil || isNull(n) {
		return false
	}
	if n.Kind == yaml.SequenceNode {
		return len(n.Content) > 0
	}
	if nodeDynamic(n) {
		return true
	}
	if s, ok := scalarString(n); ok {
		return s != ""
	}
	return true
}

func interpretResource(node *yaml.Node, alias string, index int) resourceSubject {
	path := "resources[" + itoa(index) + "]"
	subj := resourceSubject{loc: locOf(node, alias, path), index: index}
	if node == nil || node.Kind != yaml.MappingNode {
		subj.imageInvalid = !nodeDynamic(node)
		return subj
	}
	nf := mappingName(node)
	subj.nameDynamic = nf.dynamic
	imageNode := mappingValue(node, "image")
	if imageNode != nil && !isNull(imageNode) {
		subj.imagePresent = true
		if nodeDynamic(imageNode) {
			subj.imageDynamic = true
		}
		if s, ok := scalarString(imageNode); ok {
			subj.image = s
			if s == "" {
				subj.imagePresent = false
			}
		} else {
			subj.imageInvalid = true
		}
	}
	return subj
}

func (s *declaredStack) effectivePinEnabled(subj mcpSubject) (enabled bool, unknown bool) {
	if s.pinningUnknown || s.globalEnabledUnk || subj.pinUnknown {
		return false, true
	}
	global := true
	if s.globalEnabled != nil {
		global = *s.globalEnabled
	}
	if !global {
		return false, false
	}
	if subj.pinSchemas != nil {
		return *subj.pinSchemas, false
	}
	return true, false
}

func (s *declaredStack) effectivePinAction() (action string, unknown bool) {
	if s.pinningUnknown || s.globalActionUnk {
		return "", true
	}
	if s.globalAction == "" {
		return "warn", false
	}
	if s.globalAction == "block" {
		return "block", false
	}
	return "warn", false
}
