package api

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// loopbackRequest builds a request that looks like it arrived over loopback.
//
// httptest.NewRequest defaults Host to example.com, which the DNS rebinding
// protection in Handler() rejects with 403 before any handler runs. Tests in
// this package build requests through here rather than calling
// httptest.NewRequest directly, so a foreign Host stays a deliberate choice
// made by the tests that actually exercise the gate.
func loopbackRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "localhost:8180"
	return r
}
