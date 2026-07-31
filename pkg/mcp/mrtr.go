package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// MRTR (Multi Round-Trip Requests, 2026-07-28) relay support.
//
// When a modern downstream server returns resultType "input_required",
// its requestState is opaque state meaningful only to that server; the
// client must echo it byte-exact on the retry. gridctl relays MRTR
// results between same-generation peers without inspecting them, but it
// rewrites tool names (server__tool), so the retry must be routable back
// to the origin server and the origin's exact bytes must be restored.
// The envelope below records the canonical server name around the origin
// state on the upstream leg; the retry unwraps it on the way back down.
//
// The envelope carries routing information only. Integrity of the inner
// state is the origin server's responsibility (the spec requires servers
// to treat requestState as attacker-controlled input); a tampered
// envelope can at worst misroute the retry, which fails loudly when the
// recorded server does not match the routed target.

// mrtrEnvelopePrefix self-identifies gridctl-wrapped request state. The
// spec sanctions any server-chosen encoding, and clients must treat the
// whole value as opaque, so the prefix never collides with client
// expectations.
const mrtrEnvelopePrefix = "gridctl-mrtr-v1:"

type mrtrEnvelope struct {
	Server string `json:"server"`
	// State is base64 of the origin server's exact requestState bytes.
	// JSON string marshaling sanitizes invalid UTF-8, so the inner
	// value is base64-wrapped to guarantee byte-exactness regardless
	// of what encoding the origin chose.
	State string `json:"state"`
}

// wrapRequestState wraps an origin server's requestState in the gridctl
// routing envelope.
func wrapRequestState(server, originState string) string {
	env := mrtrEnvelope{
		Server: server,
		State:  base64.StdEncoding.EncodeToString([]byte(originState)),
	}
	payload, err := json.Marshal(env)
	if err != nil {
		// Two strings cannot fail to marshal; guard for Article V anyway.
		return mrtrEnvelopePrefix
	}
	return mrtrEnvelopePrefix + base64.StdEncoding.EncodeToString(payload)
}

// unwrapRequestState recovers the origin server name and its exact
// state bytes from a gridctl envelope. ok is false when the value is
// not a gridctl envelope (including client-corrupted ones); callers
// reject the retry rather than forwarding unroutable state.
func unwrapRequestState(wrapped string) (server, originState string, ok bool) {
	rest, found := strings.CutPrefix(wrapped, mrtrEnvelopePrefix)
	if !found {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", "", false
	}
	var env mrtrEnvelope
	if err := json.Unmarshal(decoded, &env); err != nil || env.Server == "" {
		return "", "", false
	}
	stateBytes, err := base64.StdEncoding.DecodeString(env.State)
	if err != nil {
		return "", "", false
	}
	return env.Server, string(stateBytes), true
}
