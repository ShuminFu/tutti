package modelgateway

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"
)

const gatewayReasoningEncryptedPrefix = "tutti.reasoning.v1:"

var errInvalidGatewayReasoningReplay = errors.New("invalid gateway reasoning replay")

func requestIncludesReasoningEncryptedContent(request responsesRequest) bool {
	for _, include := range request.Include {
		if include == "reasoning.encrypted_content" {
			return true
		}
	}
	return false
}

func encodeReasoningEncryptedContent(text string) string {
	return gatewayReasoningEncryptedPrefix + base64.RawURLEncoding.EncodeToString([]byte(text))
}

func decodeReasoningEncryptedContent(encoded string) (string, bool, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, gatewayReasoningEncryptedPrefix) {
		return "", false, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(encoded, gatewayReasoningEncryptedPrefix),
	)
	if err != nil || !utf8.Valid(decoded) {
		return "", true, errInvalidGatewayReasoningReplay
	}
	return string(decoded), true, nil
}
