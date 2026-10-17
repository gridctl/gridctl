package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// unknownFieldTypeRe matches yaml.v3 KnownFields failures.
var unknownFieldTypeRe = regexp.MustCompile(`^line (\d+): field (.+) not found in type .+$`)

// unknownFieldMessageRe matches the explicit unknown-field errors returned by
// custom UnmarshalYAML implementations. Node.Decode is not strict, so those
// types check their own keys and DecodeStack turns the result into a path.
var unknownFieldMessageRe = regexp.MustCompile(`unknown field "([^"]+)" \(line (\d+)\)`)

// lineInMessageRe extracts a line number embedded in a validation message.
var lineInMessageRe = regexp.MustCompile(`\(line (\d+)\)`)

// DecodeStack decodes user-authored stack YAML and rejects keys the schema
// does not declare. Empty input decodes to an empty stack, matching
// yaml.Unmarshal. Syntax errors are returned unchanged. Unknown keys and
// other type errors are ValidationErrors.
func DecodeStack(data []byte) (*Stack, error) {
	var stack Stack
	if err := decodeStrict(data, &stack); err != nil {
		return nil, err
	}
	return &stack, nil
}

// DecodeMCPServer decodes one server snippet with the same strict rules.
func DecodeMCPServer(data []byte) (*MCPServer, error) {
	var server MCPServer
	if err := decodeStrict(data, &server); err != nil {
		return nil, err
	}
	return &server, nil
}

// DecodeResource decodes one resource snippet with the same strict rules.
func DecodeResource(data []byte) (*Resource, error) {
	var resource Resource
	if err := decodeStrict(data, &resource); err != nil {
		return nil, err
	}
	return &resource, nil
}

func decodeStrict(data []byte, dest any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dest); err != nil {
		// A comment-only document has no value. yaml.Unmarshal treats that
		// as an empty struct; Decoder.Decode returns io.EOF. Match the old
		// behavior so validate still reports required fields instead of EOF.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return classifyDecodeError(err, &doc, reflect.TypeOf(dest).Elem())
	}
	return nil
}

func classifyDecodeError(err error, doc *yaml.Node, root reflect.Type) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		var errs ValidationErrors
		for _, entry := range te.Errors {
			errs = append(errs, typeErrorToValidation(entry, doc, root))
		}
		if len(errs) == 0 {
			return err
		}
		return errs
	}
	if ve := unknownFieldsFromError(err, doc, root); len(ve) > 0 {
		return ve
	}
	return err
}

func typeErrorToValidation(entry string, doc *yaml.Node, root reflect.Type) ValidationError {
	match := unknownFieldTypeRe.FindStringSubmatch(entry)
	if match == nil {
		return ValidationError{Field: "yaml", Message: entry}
	}
	line, _ := strconv.Atoi(match[1])
	key := match[2]
	return unknownFieldError(doc, root, line, key)
}

func unknownFieldsFromError(err error, doc *yaml.Node, root reflect.Type) ValidationErrors {
	matches := unknownFieldMessageRe.FindAllStringSubmatch(err.Error(), -1)
	if len(matches) == 0 {
		return nil
	}
	var errs ValidationErrors
	seen := map[string]bool{}
	for _, match := range matches {
		line, _ := strconv.Atoi(match[2])
		key := match[1]
		id := fmt.Sprintf("%d\x00%s", line, key)
		if seen[id] {
			continue
		}
		seen[id] = true
		errs = append(errs, unknownFieldError(doc, root, line, key))
	}
	return errs
}

func unknownFieldError(doc *yaml.Node, root reflect.Type, line int, key string) ValidationError {
	field := key
	if path, ok := locateKeyPath(doc, root, line, key); ok {
		field = path
	}
	return ValidationError{
		Field:   field,
		Message: fmt.Sprintf("unknown field %q (line %d)", key, line),
	}
}

// ToIssue converts a validation error into an issue. Strict-decode messages
// that carry "(line N)" set Line.
func (e ValidationError) ToIssue(severity IssueSeverity) ValidationIssue {
	issue := ValidationIssue{
		Field:    e.Field,
		Message:  e.Message,
		Severity: severity,
	}
	if match := lineInMessageRe.FindStringSubmatch(e.Message); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil {
			issue.Line = n
		}
	}
	return issue
}

// ResultFromErrors builds a failed validation result from decode or validate errors.
func ResultFromErrors(errs ValidationErrors) *ValidationResult {
	result := &ValidationResult{Valid: false}
	for _, err := range errs {
		result.Issues = append(result.Issues, err.ToIssue(SeverityError))
		result.ErrorCount++
	}
	if result.Issues == nil {
		result.Issues = []ValidationIssue{}
	}
	return result
}

func locateKeyPath(doc *yaml.Node, root reflect.Type, line int, key string) (string, bool) {
	node := doc
	if node != nil && node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	return walkKeyPath(node, root, "", line, key)
}

func walkKeyPath(node *yaml.Node, typ reflect.Type, path string, line int, key string) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind == yaml.AliasNode {
		return walkKeyPath(node.Alias, typ, path, line, key)
	}
	typ = derefType(typ)
	switch node.Kind {
	case yaml.MappingNode:
		if typ.Kind() == reflect.Map {
			elem := typ.Elem()
			for i := 0; i+1 < len(node.Content); i += 2 {
				k, v := node.Content[i], node.Content[i+1]
				child := joinIndex(path, k.Value)
				if k.Line == line && k.Value == key {
					return child, true
				}
				if p, ok := walkKeyPath(v, elem, child, line, key); ok {
					return p, true
				}
			}
			return "", false
		}
		if typ.Kind() != reflect.Struct {
			return "", false
		}
		fields := yamlFields(typ)
		for i := 0; i+1 < len(node.Content); i += 2 {
			k, v := node.Content[i], node.Content[i+1]
			if k.Value == "<<" {
				if p, ok := walkKeyPath(v, typ, path, line, key); ok {
					return p, true
				}
				continue
			}
			child := joinField(path, k.Value)
			if k.Line == line && k.Value == key {
				return child, true
			}
			ft, ok := fields[k.Value]
			if !ok {
				continue
			}
			if p, ok := walkKeyPath(v, ft, child, line, key); ok {
				return p, true
			}
		}
	case yaml.SequenceNode:
		elem := typ
		if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			elem = typ.Elem()
		}
		for i, item := range node.Content {
			child := joinIndex(path, strconv.Itoa(i))
			if p, ok := walkKeyPath(item, elem, child, line, key); ok {
				return p, true
			}
		}
	}
	return "", false
}

func joinField(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

func joinIndex(parent, key string) string {
	if parent == "" {
		return "[" + key + "]"
	}
	return parent + "[" + key + "]"
}

func derefType(t reflect.Type) reflect.Type {
	if t == nil {
		return t
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func yamlFields(typ reflect.Type) map[string]reflect.Type {
	typ = derefType(typ)
	out := map[string]reflect.Type{}
	if typ == nil || typ.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			for name, ft := range yamlFields(field.Type) {
				if _, exists := out[name]; !exists {
					out[name] = ft
				}
			}
			continue
		}
		if field.PkgPath != "" {
			continue
		}
		name, ok := yamlFieldName(field)
		if !ok {
			continue
		}
		out[name] = field.Type
	}
	return out
}

func yamlFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("yaml")
	if tag == "-" {
		return "", false
	}
	if tag == "" {
		return strings.ToLower(field.Name), true
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		if name == "-" {
			return "", false
		}
		return strings.ToLower(field.Name), true
	}
	return name, true
}
