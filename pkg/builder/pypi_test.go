package builder

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestInspectWheelMetadata(t *testing.T) {
	wheel := makeWheel(t, map[string]string{
		"demo-1.2.3.dist-info/METADATA":         "Metadata-Version: 2.4\nName: demo\nVersion: 1.2.3\nRequires-Python: >=3.11\n",
		"demo-1.2.3.dist-info/entry_points.txt": "[console_scripts]\nzebra = demo:zebra\nalpha = demo:main\n[other]\nignored = demo:no\n",
		"demo/__init__.py":                      "raise RuntimeError('must not execute')\n",
		"demo/data.bin":                         strings.Repeat("x", 2<<20),
	})
	metadata, err := InspectWheelMetadata(context.Background(), wheel)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "demo" || metadata.Version != "1.2.3" || metadata.RequiresPython != ">=3.11" {
		t.Fatalf("metadata = %+v", metadata)
	}
	if !reflect.DeepEqual(metadata.ConsoleScripts, []string{"alpha", "zebra"}) {
		t.Fatalf("scripts = %v", metadata.ConsoleScripts)
	}
}

func TestInspectWheelMetadata_RejectsOversizedMetadata(t *testing.T) {
	wheel := makeWheel(t, map[string]string{
		"demo-1.0.dist-info/METADATA": "Name: demo\nVersion: 1.0\n" + strings.Repeat("x", 1<<20),
	})
	if _, err := InspectWheelMetadata(context.Background(), wheel); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("oversized metadata error = %v", err)
	}
}

func TestInspectWheelMetadata_RejectsTraversal(t *testing.T) {
	wheel := makeWheel(t, map[string]string{
		"../demo.dist-info/METADATA": "Name: demo\nVersion: 1.0\n",
	})
	if _, err := InspectWheelMetadata(context.Background(), wheel); err == nil {
		t.Fatal("traversal path accepted")
	}
}

func TestPyPIResolver_ExactRelease(t *testing.T) {
	projectRequests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo-server/1.2.3/json" {
			projectRequests++
			_, _ = w.Write([]byte(strings.Repeat("x", maxPyPIMetadata+1)))
			return
		}
		payload := map[string]any{
			"info": map[string]any{"name": "Demo_Server", "version": "1.2.3", "requires_python": ">=3.11,<3.13"},
			"urls": []map[string]any{{
				"filename": "demo-server-1.2.3.tar.gz", "packagetype": "sdist",
				"url": server.URL + "/files/demo.tar.gz", "size": 10, "yanked": false,
				"digests": map[string]any{"sha256": strings.Repeat("a", 64)},
			}},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()
	resolver := NewPyPIResolver(server.Client())
	resolver.baseURL = server.URL
	release, err := resolver.Resolve(context.Background(), "demo-server", "1.2.3", "")
	if err != nil {
		t.Fatal(err)
	}
	if release.Package != "Demo_Server" || release.Version != "1.2.3" || release.Python != "3.11" || release.Artifact.Packagetype != "sdist" {
		t.Fatalf("release = %+v", release)
	}
	if projectRequests != 0 {
		t.Fatalf("project-wide metadata requested %d times", projectRequests)
	}
}

func TestPyPIResolver_VerifiesAndInspectsWheel(t *testing.T) {
	wheel := makeWheel(t, map[string]string{
		"demo-1.0.dist-info/METADATA":         "Name: demo\nVersion: 1.0\nRequires-Python: >=3.13\n",
		"demo-1.0.dist-info/entry_points.txt": "[console_scripts]\ndemo = demo:main\n",
	})
	digest := sha256.Sum256(wheel)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/demo.whl" {
			_, _ = w.Write(wheel)
			return
		}
		payload := map[string]any{
			"info": map[string]any{"name": "demo", "version": "1.0", "requires_python": ">=3.11"},
			"urls": []map[string]any{{
				"filename": "demo-1.0-py3-none-any.whl", "packagetype": "bdist_wheel",
				"url": server.URL + "/files/demo.whl", "size": len(wheel), "yanked": false,
				"digests": map[string]any{"sha256": hex.EncodeToString(digest[:])},
			}},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()
	resolver := NewPyPIResolver(server.Client())
	resolver.baseURL = server.URL
	release, err := resolver.Resolve(context.Background(), "demo", "1.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(release.Metadata.ConsoleScripts, []string{"demo"}) {
		t.Fatalf("metadata = %+v", release.Metadata)
	}
	if release.RequiresPython != ">=3.13" || release.Python != "3.13" {
		t.Fatalf("wheel Python constraint was not selected: %+v", release)
	}
	if _, err := resolver.Resolve(context.Background(), "demo", "1.0", "3.12"); err == nil || !strings.Contains(err.Error(), "requires Python >=3.13") {
		t.Fatalf("incompatible explicit Python error = %v", err)
	}

	badDigest := strings.Repeat("0", 64)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/demo.whl" {
			_, _ = w.Write(wheel)
			return
		}
		_, _ = w.Write([]byte(`{"info":{"name":"demo","version":"1.0"},"urls":[{"filename":"demo-1.0-py3-none-any.whl","packagetype":"bdist_wheel","url":"` + server.URL + `/files/demo.whl","size":` + strconv.Itoa(len(wheel)) + `,"yanked":false,"digests":{"sha256":"` + badDigest + `"}}]}`))
	})
	if _, err := resolver.Resolve(context.Background(), "demo", "1.0", ""); err == nil || !strings.Contains(err.Error(), "SHA-256 digest") {
		t.Fatalf("digest error = %v", err)
	}
}

func TestPyPIResolver_ErrorContracts(t *testing.T) {
	tests := []struct {
		releaseStatus  int
		releasePayload string
		projectStatus  int
		projectPayload string
		want           string
	}{
		{http.StatusNotFound, `{}`, http.StatusNotFound, `{}`, "No PyPI project named missing."},
		{http.StatusNotFound, `{}`, http.StatusOK, `{"info":{"name":"missing","version":"2.0"}}`, "1.0 is not a published version of missing. Latest is 2.0."},
		{http.StatusOK, `{"info":{"name":"missing","version":"1.0"},"urls":[{"filename":"x.tar.gz","packagetype":"sdist","url":"https://files.pythonhosted.org/x","size":1,"yanked":true,"digests":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]}`, 0, ``, "1.0 of missing is yanked on PyPI. Choose a non-yanked version."},
	}
	for _, test := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/missing/1.0/json" {
				w.WriteHeader(test.releaseStatus)
				_, _ = w.Write([]byte(test.releasePayload))
				return
			}
			w.WriteHeader(test.projectStatus)
			_, _ = w.Write([]byte(test.projectPayload))
		}))
		resolver := NewPyPIResolver(server.Client())
		resolver.baseURL = server.URL
		_, err := resolver.Resolve(context.Background(), "missing", "1.0", "")
		server.Close()
		if err == nil || err.Error() != test.want {
			t.Errorf("error = %v, want %q", err, test.want)
		}
	}
}

func TestPyPIResolver_RejectsNonExactVersion(t *testing.T) {
	resolver := NewPyPIResolver(nil)
	for _, version := range []string{"latest", ">=1.0", "1.*", ""} {
		if _, err := resolver.Resolve(context.Background(), "demo", version, ""); err == nil {
			t.Errorf("version %q accepted", version)
		}
	}
}

func makeWheel(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
