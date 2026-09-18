package modelgateway

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Upstream `reasoning_content` is the one part of the Chat Completions contract
// that the Responses client has to hand back verbatim: in thinking mode the
// upstream rejects a request whose replayed assistant turn dropped it. The
// gateway is the only party that ever saw it, so it hands the text to the client
// inside a gateway-private envelope carried on the reasoning item's
// `encrypted_content`. That envelope is the only copy that survives a
// client-side history rewrite (auto-compaction, `/compact`, resume), which is
// exactly the case where the client can no longer replay the plaintext.
//
// Layout: <prefix><session-id-b64>:<reasoning-text-b64>. The session segment
// binds an envelope to the agent session that produced it, so a reasoning blob
// minted for one thread is rejected rather than silently replayed into another.
//
// There is deliberately no compatibility path for an unscoped envelope: the
// scoped format is the first one to reach a running build, so no client can be
// holding an older shape.
const gatewayReasoningEncryptedPrefix = "tutti.reasoning.v1:"

var errInvalidGatewayReasoningReplay = errors.New("invalid gateway reasoning replay")

// reasoningScope is the gateway session identity an envelope is sealed to. The
// zero value seals nothing usable, which keeps callers that have no route
// (unit tests of the pure converters) explicit about the loss.
type reasoningScope struct {
	agentSessionID string
}

func newReasoningScope(agentSessionID string) reasoningScope {
	return reasoningScope{agentSessionID: strings.TrimSpace(agentSessionID)}
}

func (s reasoningScope) sealable() bool { return s.agentSessionID != "" }

// seal encodes upstream reasoning for replay by the client. It returns nil when
// the scope cannot identify a session: an unbound envelope would be replayable
// anywhere, so the gateway reports "no encrypted content" instead.
func (s reasoningScope) seal(text string) any {
	if !s.sealable() {
		return nil
	}
	session := base64.RawURLEncoding.EncodeToString([]byte(s.agentSessionID))
	body := base64.RawURLEncoding.EncodeToString([]byte(text))
	return gatewayReasoningEncryptedPrefix + session + ":" + body
}

// open reverses seal. handled is false when the value is not a gateway envelope
// at all — the caller must keep rejecting foreign encrypted content rather than
// forwarding it upstream as reasoning.
func (s reasoningScope) open(encoded string) (text string, handled bool, err error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, gatewayReasoningEncryptedPrefix) {
		return "", false, nil
	}
	payload := strings.TrimPrefix(encoded, gatewayReasoningEncryptedPrefix)
	session, body, found := strings.Cut(payload, ":")
	if !found {
		return "", true, errInvalidGatewayReasoningReplay
	}
	decodedSession, err := base64.RawURLEncoding.DecodeString(session)
	if err != nil || !utf8.Valid(decodedSession) {
		return "", true, errInvalidGatewayReasoningReplay
	}
	if !s.sealable() || string(decodedSession) != s.agentSessionID {
		// A well-formed envelope from another session. Replaying it here would
		// feed this thread reasoning it never produced.
		return "", true, fmt.Errorf(
			"%w: envelope belongs to another agent session", errInvalidGatewayReasoningReplay,
		)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || !utf8.Valid(decoded) {
		return "", true, errInvalidGatewayReasoningReplay
	}
	return string(decoded), true, nil
}

func requestIncludesReasoningEncryptedContent(request responsesRequest) bool {
	for _, include := range request.Include {
		if include == "reasoning.encrypted_content" {
			return true
		}
	}
	return false
}

// reasoningReplayStats is the gateway's own account of how much upstream
// reasoning it could reconstruct for one request. A miss is the leading
// indicator of the upstream "reasoning_content must be passed back" rejection,
// and it is invisible in the request the client sent, so it has to be counted
// here and reported on the failure path. Items add up as plain + sealed + empty.
type reasoningReplayStats struct {
	// items is every reasoning item the client replayed.
	items int
	// plain is how many still carried their readable text.
	plain int
	// sealed is how many the client could only hand back as a gateway envelope,
	// i.e. the cases this gateway's own replay channel recovered.
	sealed int
	// empty is how many carried neither: the client asserted reasoning existed
	// and the gateway cannot recover it.
	empty int
}

func (s reasoningReplayStats) lost() bool { return s.empty > 0 }

func (s reasoningReplayStats) fields() []any {
	return []any{
		"reasoning_items", s.items,
		"reasoning_plain", s.plain,
		"reasoning_sealed", s.sealed,
		"reasoning_lost", s.empty,
	}
}
