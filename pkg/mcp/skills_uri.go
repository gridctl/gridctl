package mcp

import (
	"net/url"
	"regexp"
	"strings"
)

const skillURIPrefix = "skill://registry/"

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// SkillURI formats a registry skill path using canonical segment escaping.
func SkillURI(name, path string) string {
	if path == "" {
		return skillURIPrefix + name
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return skillURIPrefix + name + "/" + strings.Join(parts, "/")
}

func parseSkillURI(uri string) (name, path string, ok bool) {
	if !strings.HasPrefix(uri, skillURIPrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(uri, skillURIPrefix), "/")
	name = parts[0]
	if len(name) > 64 || !skillNamePattern.MatchString(name) {
		return "", "", false
	}
	for i := 1; i < len(parts); i++ {
		part, err := url.PathUnescape(parts[i])
		if err != nil || part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\\x00") {
			return "", "", false
		}
		parts[i] = part
	}
	path = strings.Join(parts[1:], "/")
	if SkillURI(name, path) != uri {
		return "", "", false
	}
	return name, path, true
}
