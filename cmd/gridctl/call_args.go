package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/mcp"
)

const (
	callArgsMaxBytes = 1 << 20
	callClientMax    = 128
)

func parseCallTarget(raw string) (string, error) {
	server, tool, err := mcp.ParsePrefixedTool(raw)
	if err != nil || server == "" || tool == "" {
		return "", fmt.Errorf("target must be server__tool")
	}
	return raw, nil
}

func parseCallArguments(src string) (map[string]any, error) {
	var data []byte
	if strings.HasPrefix(src, "@") {
		path := strings.TrimPrefix(src, "@")
		if path == "" || strings.Contains(path, "://") || strings.HasPrefix(path, "-") {
			return nil, fmt.Errorf("invalid file path")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("unreadable file")
		}
		defer f.Close()
		limited, err := io.ReadAll(io.LimitReader(f, callArgsMaxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("unreadable file")
		}
		if len(limited) > callArgsMaxBytes {
			return nil, errArgsTooLarge
		}
		data = limited
	} else {
		if len(src) > callArgsMaxBytes {
			return nil, errArgsTooLarge
		}
		data = []byte(src)
	}
	return decodeArgumentObject(data)
}

var errArgsTooLarge = fmt.Errorf("arguments exceed 1 MiB")

func decodeArgumentObject(data []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("invalid JSON")
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func declaredCLIClient(raw string, present bool) (string, error) {
	if !present {
		return "cli", nil
	}
	if len(raw) > callClientMax || !utf8.ValidString(raw) {
		return "", fmt.Errorf("invalid client")
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("invalid client")
		}
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("invalid client")
	}
	if mcp.NormalizeClientID(raw) == "" {
		return "", fmt.Errorf("invalid client")
	}
	return raw, nil
}
