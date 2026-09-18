package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestExportStack_CredentialInventory(t *testing.T) {
	t.Run("inline_literal_token", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "stack.yaml")
		source := "name: test\ngateway:\n  auth:\n    token: synthetic-literal-canary\n"
		require.NoError(t, os.WriteFile(path, []byte(source), 0600))
		_, _, err := ExportStack(context.Background(), path)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "canary")
	})
	for _, field := range []string{
		"gateway:\n  auth:\n    token: ",
		"gateway:\n  tokenizer_api_key: ",
		"mcp-servers:\n  - auth:\n      token: ",
		"mcp-servers:\n  - a2a:\n      auth:\n        token: ",
		"mcp-servers:\n  - auth:\n      value: ",
		"mcp-servers:\n  - auth:\n      client_secret: ",
		"mcp-servers:\n  - source:\n      auth:\n        credential_ref: ",
		"mcp-servers:\n  - env:\n      API_KEY: ",
		"resources:\n  - env:\n      PASSWORD: ",
	} {
		for _, value := range []string{"", "$PROBE", "${PROBE}", "${var:PROBE}", "${vault:PROBE}", "prefix-${PROBE}-suffix", "${PROBE:-}", "${PROBE:+}", "synthetic-literal-canary", "${PROBE:-synthetic-default-canary}", "${PROBE:+synthetic-replacement-canary}", "${var:}"} {
			t.Run(field+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "stack.yaml")
				encoded, err := yaml.Marshal(value)
				require.NoError(t, err)
				source := "name: test\n" + field + string(encoded)
				require.NoError(t, os.WriteFile(path, []byte(source), 0600))
				_, _, err = ExportStack(context.Background(), path)
				bad := value == "synthetic-literal-canary" || value == "${PROBE:-synthetic-default-canary}" || value == "${PROBE:+synthetic-replacement-canary}" || value == "${var:}"
				if bad {
					require.Error(t, err)
					require.NotContains(t, err.Error(), "canary")
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestExportStack_PreservesStringsScopesAndDefaults(t *testing.T) {
	t.Setenv("PROBE", "synthetic-ambient-canary")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	source := `name: ${PROBE}
gateway:
  auth: {token: '${PROBE}'}
  bind: $PROBE
  allowed_hosts: ['${PROBE}']
  allowed_origins: ['${PROBE}']
  tokenizer_api_key: ${var:PROBE}
network: {name: '${PROBE}'}
networks: [{name: '${PROBE}'}]
secrets:
  sets:
    - name: empty
      servers: []
    - shared
mcp-servers:
  - name: test
    image: ${PROBE}
    network: ${PROBE}
    command: ['$PROBE', '${PROBE:-ordinary}', '${PROBE:+replacement}', 'unclassified literal']
    url: https://example.test/${PROBE}
    volumes: ['${PROBE}:/data']
    source:
      type: local
      path: ${PROBE}
      ref: ${vault:PROBE}
      dockerfile: ${var:PROBE}
      project_path: ${PROBE}
      url: ${PROBE}
      package: ${PROBE}
      python: ${PROBE}
      extras: ['${PROBE}']
      with: ['${PROBE}']
      packages: ['${PROBE}']
      auth: {credential_ref: '${var:PROBE}', ssh_key_path: '${PROBE}', ssh_user: '${PROBE}'}
    ssh: {host: '${PROBE}', user: '${PROBE}', identityFile: '${PROBE}', knownHostsFile: '${PROBE}', jumpHost: '${PROBE}'}
    openapi: {spec: '${PROBE}', baseUrl: '${PROBE}'}
    build_args: {PAYLOAD: '${PROBE}'}
    env: {PAYLOAD: '${PROBE}', TOKEN: '${PROBE}'}
    auth: {client_id: ordinary-client, token: '${PROBE}', value: '${PROBE}', client_secret: '${PROBE}'}
resources:
  - name: ${PROBE}
    image: ${PROBE}
    network: ${PROBE}
    env: {TOKEN: '${vault:PROBE}', PAYLOAD: '${PROBE}'}
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	stack, sources, err := ExportStack(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, []string{path}, sources)
	require.Equal(t, "", stack.Version, "export must not synthesize defaults")
	require.Equal(t, "${PROBE}", stack.MCPServers[0].Source.Path)
	require.Nil(t, stack.References)
	var authored Stack
	require.NoError(t, yaml.Unmarshal([]byte(source), &authored))
	require.Equal(t, authored, *stack, "every decoded field must retain its authored content")
	before, err := yaml.Marshal(stack)
	require.NoError(t, err)
	t.Setenv("PROBE", "different-synthetic-canary")
	again, _, err := ExportStack(context.Background(), path)
	require.NoError(t, err)
	after, err := yaml.Marshal(again)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, marshal := range []func(any) ([]byte, error){yaml.Marshal, json.Marshal} {
		data, err := marshal(stack)
		require.NoError(t, err)
		require.NotContains(t, string(data), "canary")
		require.NotContains(t, string(data), "References")
		require.Contains(t, string(data), "servers")
	}
	var ref SecretSetRef
	require.NoError(t, yaml.Unmarshal([]byte("name: test\nservers: []"), &ref))
	data, err := json.Marshal(ref)
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"test","servers":[]}`, string(data))
	var decoded SecretSetRef
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.True(t, decoded.Scoped())
	require.False(t, decoded.InjectsIntoServer("any"))
}

func TestExportStack_InheritanceAndSafeErrors(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.yaml")
	child := filepath.Join(dir, "child.yaml")
	require.NoError(t, os.WriteFile(parent, []byte("name: parent\nmcp-servers:\n  - name: inherited\n    source: {type: local, path: relative}\nclients: {default: allow}\n"), 0600))
	require.NoError(t, os.WriteFile(child, []byte("name: child\nextends: parent.yaml\n"), 0600))
	stack, sources, err := ExportStack(context.Background(), child)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Empty(t, stack.Extends)
	require.Nil(t, stack.Clients)
	require.Equal(t, filepath.Join(dir, "relative"), stack.MCPServers[0].Source.Path)
	for _, source := range []string{
		"mcp-servers:\n  - source: {type: local, path: '${PROBE}'}\n",
		"extends: child.yaml\n",
		"name: [synthetic-syntax-canary\n",
		"secrets: {sets: [{synthetic-key-canary: value}]}\n",
	} {
		require.NoError(t, os.WriteFile(parent, []byte(source), 0600))
		_, _, err := ExportStack(context.Background(), child)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "canary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = ExportStack(ctx, child)
	require.ErrorIs(t, err, context.Canceled)
}
