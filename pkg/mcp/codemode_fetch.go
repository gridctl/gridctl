package mcp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

// DefaultFetchMaxResponseBytes is the default response size cap (1MB).
const DefaultFetchMaxResponseBytes = 1 * 1024 * 1024

// DefaultFetchRequestTimeout is the per-request timeout for fetch calls.
const DefaultFetchRequestTimeout = 10 * time.Second

// FetchConfig holds operator-configurable settings for the sandboxed fetch client.
type FetchConfig struct {
	// HTTPSOnly rejects plain http:// URLs when true (default: true).
	HTTPSOnly bool
	// MaxResponseBytes caps the response body size. Default: 1MB.
	MaxResponseBytes int64
	// RequestTimeout is the per-request timeout. Default: 10s.
	RequestTimeout time.Duration
	// AllowPrivateNetworks disables the SSRF IP blocklist. For testing only.
	AllowPrivateNetworks bool
}

// DefaultFetchConfig returns a FetchConfig with secure defaults.
func DefaultFetchConfig() FetchConfig {
	return FetchConfig{
		HTTPSOnly:        true,
		MaxResponseBytes: DefaultFetchMaxResponseBytes,
		RequestTimeout:   DefaultFetchRequestTimeout,
	}
}

// privateIPNets are the CIDR ranges blocked by isPrivateIP.
var privateIPNets []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",    // loopback (IPv4)
		"::1/128",        // loopback (IPv6)
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local (IPv4)
		"fe80::/10",      // link-local (IPv6)
		"224.0.0.0/4",    // multicast (IPv4)
		"ff00::/8",       // multicast (IPv6)
		"0.0.0.0/8",      // unspecified
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("codemode_fetch: bad CIDR %s: %v", cidr, err))
		}
		privateIPNets = append(privateIPNets, ipNet)
	}
}

// isPrivateIP reports whether ip falls within any blocked private range.
func isPrivateIP(ip net.IP) bool {
	for _, ipNet := range privateIPNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// sandboxedFetch implements a fetch() global with SSRF mitigations.
type sandboxedFetch struct {
	config FetchConfig
	client *http.Client
}

// newSandboxedFetch builds a fetch client with a custom DialContext that validates
// resolved IPs at TCP dial time, preventing DNS rebinding attacks.
func newSandboxedFetch(config FetchConfig) *sandboxedFetch {
	return newSandboxedFetchWithTLSConfig(config, nil)
}

// newSandboxedFetchWithTLSConfig creates a fetch client with a custom TLS config.
// Used in tests to trust httptest.NewTLSServer certificates.
func newSandboxedFetchWithTLSConfig(config FetchConfig, tlsCfg *tls.Config) *sandboxedFetch {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("fetch error: invalid address %q: %w", addr, err)
			}

			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("fetch error: DNS lookup failed for %q: %w", host, err)
			}

			if !config.AllowPrivateNetworks {
				for _, ipStr := range ips {
					ip := net.ParseIP(ipStr)
					if ip == nil {
						continue
					}
					if isPrivateIP(ip) {
						return nil, fmt.Errorf("fetch blocked: private network addresses are not allowed (resolved %s)", ipStr)
					}
				}
			}

			// Dial the first resolved IP to avoid a second DNS lookup that could
			// return a different (private) address — prevents TOCTOU rebinding.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("fetch error: stopped after 10 redirects")
			}
			// DialContext validates IPs on each connection including redirects.
			return nil
		},
	}

	return &sandboxedFetch{config: config, client: client}
}

// inject registers the fetch global on the goja runtime.
// Must be called from inside loop.Run(), with vm belonging to the same loop.
func (sf *sandboxedFetch) inject(vm *goja.Runtime, loop *eventloop.EventLoop) {
	_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.NewGoError(fmt.Errorf("fetch requires a URL argument")))
		}

		url := call.Arguments[0].String()

		// Parse options (second argument, optional)
		method := "GET"
		var reqHeaders map[string]string
		var reqBody string

		if len(call.Arguments) >= 2 && !goja.IsUndefined(call.Arguments[1]) && !goja.IsNull(call.Arguments[1]) {
			if opts, ok := call.Arguments[1].Export().(map[string]any); ok {
				if m, ok := opts["method"].(string); ok && m != "" {
					method = strings.ToUpper(m)
				}
				if h, ok := opts["headers"].(map[string]any); ok {
					reqHeaders = make(map[string]string, len(h))
					for k, v := range h {
						reqHeaders[k] = fmt.Sprintf("%v", v)
					}
				}
				if b, ok := opts["body"].(string); ok {
					reqBody = b
				}
			}
		}

		promise, resolve, reject := vm.NewPromise()

		// Register a keepAlive timer to hold the event loop open while the goroutine
		// is in flight. loop.SetTimeout called inside loop.Run's fn callback queues
		// its registration via addAuxJob; loop.run(false) processes it via runAux()
		// before entering its "for jobCount > 0" loop, keeping jobCount > 0.
		// The watchdog fires only if the goroutine fails to cancel it (should not happen).
		keepAlive := loop.SetTimeout(func(*goja.Runtime) {
			_ = reject(vm.NewGoError(fmt.Errorf("fetch timeout: response not delivered")))
		}, sf.config.RequestTimeout+5*time.Second)

		capturedURL := url
		capturedMethod := method
		capturedHeaders := reqHeaders
		capturedBody := reqBody
		capturedHTTPSOnly := sf.config.HTTPSOnly

		go func() {
			deliver := func(fn func(*goja.Runtime)) {
				loop.RunOnLoop(func(vm *goja.Runtime) {
					loop.ClearTimeout(keepAlive)
					fn(vm)
				})
			}

			// HTTPS check
			if capturedHTTPSOnly && !strings.HasPrefix(capturedURL, "https://") {
				deliver(func(vm *goja.Runtime) {
					_ = reject(vm.NewGoError(fmt.Errorf("fetch blocked: only HTTPS URLs are allowed")))
				})
				return
			}

			start := time.Now()

			ctx, cancel := context.WithTimeout(context.Background(), sf.config.RequestTimeout)
			defer cancel()

			var bodyReader io.Reader
			if capturedBody != "" {
				bodyReader = strings.NewReader(capturedBody)
			}

			req, err := http.NewRequestWithContext(ctx, capturedMethod, capturedURL, bodyReader)
			if err != nil {
				deliver(func(vm *goja.Runtime) {
					_ = reject(vm.NewGoError(fmt.Errorf("fetch error: %w", err)))
				})
				return
			}

			for k, v := range capturedHeaders {
				req.Header.Set(k, v)
			}

			resp, err := sf.client.Do(req)
			if err != nil {
				deliver(func(vm *goja.Runtime) {
					_ = reject(vm.NewGoError(fmt.Errorf("fetch error: %w", err)))
				})
				return
			}
			defer resp.Body.Close()

			limited := io.LimitReader(resp.Body, sf.config.MaxResponseBytes+1)
			bodyBytes, err := io.ReadAll(limited)
			if err != nil {
				deliver(func(vm *goja.Runtime) {
					_ = reject(vm.NewGoError(fmt.Errorf("fetch error: reading response body: %w", err)))
				})
				return
			}

			if int64(len(bodyBytes)) > sf.config.MaxResponseBytes {
				deliver(func(vm *goja.Runtime) {
					_ = reject(vm.NewGoError(fmt.Errorf("fetch error: response body exceeded %d byte limit", sf.config.MaxResponseBytes)))
				})
				return
			}

			status := resp.StatusCode
			bodyStr := string(bodyBytes)
			duration := time.Since(start)

			respHeaders := make(map[string]string, len(resp.Header))
			for k := range resp.Header {
				respHeaders[strings.ToLower(k)] = resp.Header.Get(k)
			}

			slog.Default().Info("code mode fetch",
				"url", capturedURL,
				"method", capturedMethod,
				"status", status,
				"duration", duration,
			)

			deliver(func(vm *goja.Runtime) {
				respObj := vm.NewObject()
				_ = respObj.Set("ok", status >= 200 && status < 300)
				_ = respObj.Set("status", status)

				headersObj := vm.NewObject()
				for k, v := range respHeaders {
					_ = headersObj.Set(k, v)
				}
				_ = respObj.Set("headers", headersObj)

				capturedBody := bodyStr
				_ = respObj.Set("text", func(call goja.FunctionCall) goja.Value {
					p, res, _ := vm.NewPromise()
					_ = res(vm.ToValue(capturedBody))
					return vm.ToValue(p)
				})

				_ = respObj.Set("json", func(call goja.FunctionCall) goja.Value {
					p, res, rej := vm.NewPromise()
					var parsed any
					if jsonErr := json.Unmarshal([]byte(capturedBody), &parsed); jsonErr != nil {
						_ = rej(vm.NewGoError(fmt.Errorf("fetch error: JSON parse failed: %w", jsonErr)))
					} else {
						_ = res(vm.ToValue(parsed))
					}
					return vm.ToValue(p)
				})

				_ = resolve(respObj)
			})
		}()

		return vm.ToValue(promise)
	})
}
