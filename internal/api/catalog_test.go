package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCatalogRegistry swaps the registry seam for the test's lifetime.
func stubCatalogRegistry(t *testing.T, fn func(ctx context.Context, query string) ([]catalog.Entry, bool, error)) {
	t.Helper()
	orig := catalogRegistrySearch
	catalogRegistrySearch = fn
	t.Cleanup(func() { catalogRegistrySearch = orig })
}

func getCatalog(t *testing.T, target string) (int, catalogResponse) {
	t.Helper()
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	s.handleCatalog(w, req)
	var resp catalogResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w.Code, resp
}

func TestHandleCatalog_EmptyQueryListsCuratedOnly(t *testing.T) {
	called := false
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		called = true
		return nil, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog")

	assert.Equal(t, http.StatusOK, code)
	assert.False(t, called, "empty query must not contact the registry")
	assert.NotEmpty(t, resp.Servers)
	for _, e := range resp.Servers {
		assert.Equal(t, catalog.TierCurated, e.Tier)
	}
}

func TestHandleCatalog_QueryFiltersCurated(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return nil, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=github")

	assert.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, resp.Servers)
	assert.Equal(t, "github", resp.Servers[0].Name)
	assert.Equal(t, "github", resp.Query)
}

func TestHandleCatalog_MergesRegistryAfterCurated(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return []catalog.Entry{
			{Name: "io.example/gitlab-extra", Description: "community gitlab server", Tier: catalog.TierRegistry, Status: catalog.StatusActive},
		}, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=gitlab")

	assert.Equal(t, http.StatusOK, code)
	require.GreaterOrEqual(t, len(resp.Servers), 2)
	assert.Equal(t, catalog.TierCurated, resp.Servers[0].Tier, "curated entries sort first")
	last := resp.Servers[len(resp.Servers)-1]
	assert.Equal(t, catalog.TierRegistry, last.Tier)
	assert.Empty(t, resp.RegistryError)
	assert.False(t, resp.Stale)
}

func TestHandleCatalog_RegistryErrorDegradesToCurated(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return nil, false, errors.New("connection refused")
	})

	code, resp := getCatalog(t, "/api/catalog?q=github")

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "connection refused", resp.RegistryError)
	assert.NotEmpty(t, resp.Servers, "curated results survive a registry outage")
}

func TestHandleCatalog_StaleCacheFlagged(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return []catalog.Entry{
			{Name: "io.example/cached", Description: "from stale cache", Tier: catalog.TierRegistry, Status: catalog.StatusActive},
		}, true, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=cached")

	assert.Equal(t, http.StatusOK, code)
	assert.True(t, resp.Stale)
}

func TestHandleCatalog_SourceCuratedSkipsRegistry(t *testing.T) {
	called := false
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		called = true
		return nil, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=github&source=curated")

	assert.Equal(t, http.StatusOK, code)
	assert.False(t, called)
	for _, e := range resp.Servers {
		assert.Equal(t, catalog.TierCurated, e.Tier)
	}
}

func TestHandleCatalog_SourceRegistryOnly(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return []catalog.Entry{
			{Name: "io.example/thing", Description: "registry thing", Tier: catalog.TierRegistry, Status: catalog.StatusActive},
		}, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=thing&source=registry")

	assert.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Servers, 1)
	assert.Equal(t, catalog.TierRegistry, resp.Servers[0].Tier)
}

func TestHandleCatalog_InvalidSourceRejected(t *testing.T) {
	code, _ := getCatalog(t, "/api/catalog?source=bogus")
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestHandleCatalog_ScrubsSecretDefaults(t *testing.T) {
	stubCatalogRegistry(t, func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return []catalog.Entry{
			{
				Name: "io.example/leaky", Description: "secret default", Tier: catalog.TierRegistry, Status: catalog.StatusActive,
				Inputs: []catalog.Input{
					{Name: "API_TOKEN", Secret: true, Default: "hunter2"},
					{Name: "REGION", Default: "us-east-1"},
				},
			},
		}, false, nil
	})

	code, resp := getCatalog(t, "/api/catalog?q=leaky&source=registry")

	assert.Equal(t, http.StatusOK, code)
	require.Len(t, resp.Servers, 1)
	require.Len(t, resp.Servers[0].Inputs, 2)
	assert.Empty(t, resp.Servers[0].Inputs[0].Default, "secret default must be scrubbed")
	assert.Equal(t, "us-east-1", resp.Servers[0].Inputs[1].Default, "non-secret default preserved")
}

func TestScrubSecretDefaults_ClonesBeforeMutation(t *testing.T) {
	shared := []catalog.Input{{Name: "TOKEN", Secret: true, Default: "leak"}}
	entries := []catalog.Entry{{Name: "a", Inputs: shared}}

	out := scrubSecretDefaults(entries)

	assert.Empty(t, out[0].Inputs[0].Default)
	assert.Equal(t, "leak", shared[0].Default, "original slice must not be mutated")
}
