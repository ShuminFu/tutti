package agentruntime

import (
	"strings"
	"testing"
)

// localPoolDisabledBody is the verbatim body a host-local account pool returns
// when every account is disabled; codex 0.144.4 hands it to the app-server
// client as the turn error message.
const localPoolDisabledBody = `{"error":{"code":"local_pool_unavailable","message":"本地账号池里的账号都已停用，请到 设置 → 本地凭证 启用至少一个。","type":"invalid_request_error"}}`

const localPoolDisabledReadable = "本地账号池里的账号都已停用，请到 设置 → 本地凭证 启用至少一个。 (local_pool_unavailable)"

func TestProviderErrorReadableDetailExtractsUpstreamEnvelope(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "compact body", raw: localPoolDisabledBody, want: localPoolDisabledReadable},
		{
			// codex re-serializes the body with spaces when it reports it directly.
			name: "spaced body",
			raw:  `{"error": {"code": "local_pool_unavailable", "message": "本地账号池里的账号都已停用，请到 设置 → 本地凭证 启用至少一个。", "type": "invalid_request_error"}}`,
			want: localPoolDisabledReadable,
		},
		{
			name: "status prefix and trailing suffix",
			raw:  `unexpected status 503 Service Unavailable: {"error":{"message":"本地账号池暂时没有可用账号，37 秒后可重试。","code":"local_pool_unavailable"}}, request id: abc`,
			want: "unexpected status 503 Service Unavailable: 本地账号池暂时没有可用账号，37 秒后可重试。 (local_pool_unavailable)",
		},
		{
			name: "code already named in message",
			raw:  `{"error":{"code":"model_not_found","message":"model_not_found: unknown provider for model gpt-x"}}`,
			want: "model_not_found: unknown provider for model gpt-x",
		},
		{name: "string error field", raw: `{"error":"upstream exploded"}`, want: "upstream exploded"},
		{name: "top-level message", raw: `{"message":"bad gateway","code":502}`, want: "bad gateway (502)"},
		{name: "plain text is kept", raw: "provider available but exploded", want: "provider available but exploded"},
		{name: "json without message is kept", raw: `{"status":"broken"}`, want: `{"status":"broken"}`},
		{name: "empty", raw: "  \n", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ProviderErrorReadableDetail(tc.raw); got != tc.want {
				t.Fatalf("ProviderErrorReadableDetail(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestProviderErrorReadableDetailMasksCredentials(t *testing.T) {
	t.Parallel()

	raw := `{"error":{"message":"rejected Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig for key sk-proj-abcdefghijklmnop api_key=supersecretvalue123"}}`
	got := ProviderErrorReadableDetail(raw)
	for _, secret := range []string{"eyJhbGciOiJIUzI1NiJ9", "sk-proj-abcdefghijklmnop", "supersecretvalue123"} {
		if strings.Contains(got, secret) {
			t.Fatalf("detail %q leaks %q", got, secret)
		}
	}
	if !strings.Contains(got, "rejected") {
		t.Fatalf("detail %q lost the readable reason", got)
	}
}

func TestProviderErrorReadableDetailCapsAtPublishedLimit(t *testing.T) {
	t.Parallel()

	raw := `{"error":{"message":"` + strings.Repeat("账号", 200) + `"}}`
	got := ProviderErrorReadableDetail(raw)
	if len(got) > CanonicalTurnErrorDetailLimit {
		t.Fatalf("detail length = %d, want at most %d", len(got), CanonicalTurnErrorDetailLimit)
	}
	if !strings.HasSuffix(got, canonicalTurnErrorDetailEllipsis) {
		t.Fatalf("detail = %q, want an explicit truncation marker", got)
	}
}

// An unclassified upstream rejection is stored as provider_error with the raw
// body in the message. The read-side projection must hand back the readable
// sentence as detail — it is the only explanation such a failure has.
func TestProjectStoredTurnErrorExposesReadableProviderErrorDetail(t *testing.T) {
	t.Parallel()

	code, detail := ProjectStoredTurnError("provider_error", localPoolDisabledBody)
	if code != "provider_error" {
		t.Fatalf("code = %q, want provider_error", code)
	}
	if detail != localPoolDisabledReadable {
		t.Fatalf("detail = %q, want %q", detail, localPoolDisabledReadable)
	}
	if got := visibleFailureCode(localPoolDisabledBody); got != "provider_error" {
		t.Fatalf("visibleFailureCode(pool body) = %q, want provider_error", got)
	}
}
