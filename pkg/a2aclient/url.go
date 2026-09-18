// Package a2aclient implements the bounded outbound A2A wire protocol.
package a2aclient

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ParseURL validates an operator or advertised A2A destination without echoing it.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || strings.Contains(raw, "#") {
		return nil, errors.New("a2a: invalid destination URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || !fixtureHost(host)) {
		return nil, errors.New("a2a: HTTPS required except loopback fixtures")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("a2a: invalid destination port")
		}
		port = strconv.Itoa(n)
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("a2a: invalid destination port")
	}
	if port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http" {
		port = ""
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

func fixtureHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && a.Host == b.Host
}
