package agentruntime

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func TestVisibleFailureCodeClassifiesDeadlineExceededAsRequestTimedOut(t *testing.T) {
	if got := visibleFailureCode("context deadline exceeded"); got != "request_timed_out" {
		t.Fatalf("visibleFailureCode() = %q, want request_timed_out", got)
	}
}

func TestIsAuthenticationRequiredClassifiesGeminiMissingAPIKey(t *testing.T) {
	err := errors.New("Gemini API key is missing or not configured")
	if !IsAuthenticationRequired(err) {
		t.Fatalf("IsAuthenticationRequired(%q) = false, want true", err)
	}
}

func TestIsAuthenticationRequiredDoesNotHideAccountFailures(t *testing.T) {
	for _, detail := range []string{
		`Kimi Code models endpoint rejected OAuth credentials: error, status code: 402, message: We're unable to verify your membership benefits at this time. Please ensure your membership is active.`,
		`Kimi Code models endpoint rejected the API key: error, status code: 401, message: Your current subscription does not have access to kimi-for-coding-highspeed.`,
		`Kimi Code request rejected OAuth credentials: error, status code: 403, message: You've reached your usage limit for this billing cycle.`,
		`Kimi Code request rejected OAuth credentials: 402 Payment Required`,
	} {
		err := errors.New(detail)
		if IsAuthenticationRequired(err) {
			t.Fatalf("IsAuthenticationRequired(%q) = true, want false", detail)
		}
		if ClassifyAccountFailure(err) == "" {
			t.Fatalf("ClassifyAccountFailure(%q) = empty, want account-state code", detail)
		}
	}
}

func TestVisibleFailureCodeClassifiesProviderConcurrencyLimit(t *testing.T) {
	detail := `stream disconnected before completion: Concurrency limit exceeded for user, please retry later`
	if got := visibleFailureCode(detail); got != "provider_concurrency_limit" {
		t.Fatalf("visibleFailureCode() = %q, want provider_concurrency_limit", got)
	}
}

func TestVisibleFailureCodeClassifiesConfigTimeout(t *testing.T) {
	detail := `agent session ACP effort configuration failed: acp session/set_config_option timed out after 30s`
	if got := visibleFailureCode(detail); got != "provider_config_timeout" {
		t.Fatalf("visibleFailureCode() = %q, want provider_config_timeout", got)
	}
}

func TestVisibleFailureContentDescribesStartupConfigTimeout(t *testing.T) {
	got := visibleFailureContent(ProviderCodex, "start", "provider_config_timeout")
	want := "Codex could not apply session settings before startup timed out. Try again in a moment."
	if got != want {
		t.Fatalf("visibleFailureContent() = %q, want %q", got, want)
	}
}

func TestVisibleFailureCodeClassifiesStreamDisconnected(t *testing.T) {
	detail := `stream disconnected before completion: Transport error: network error: error decoding response body`
	if got := visibleFailureCode(detail); got != "provider_stream_disconnected" {
		t.Fatalf("visibleFailureCode() = %q, want provider_stream_disconnected", got)
	}
}

func TestVisibleFailureCodeClassifiesProviderEmptyResponse(t *testing.T) {
	detail := "provider_empty_response: ACP agent ended the turn without assistant output or tool activity"
	if got := visibleFailureCode(detail); got != "provider_empty_response" {
		t.Fatalf("visibleFailureCode() = %q, want provider_empty_response", got)
	}
	got := visibleFailureContent("acp:kimi-code", "turn", "provider_empty_response")
	want := "Agent returned no response. Check the provider settings or try again."
	if got != want {
		t.Fatalf("visibleFailureContent() = %q, want %q", got, want)
	}
}

func TestVisibleFailureCodeClassifiesProviderPlanAndBalanceFailures(t *testing.T) {
	for _, tt := range []struct {
		detail string
		want   string
	}{
		{"Membership expired, please renew your plan", FailureCodeSubscriptionRequired},
		{"Your account has insufficient balance", FailureCodeInsufficientCredits},
		{"Account balance is insufficient", FailureCodeInsufficientCredits},
	} {
		if got := visibleFailureCode(tt.detail); got != tt.want {
			t.Fatalf("visibleFailureCode(%q) = %q, want %q", tt.detail, got, tt.want)
		}
	}
}

func TestVisibleFailureCodeDoesNotTreatPatchContextLoginTextAsAuth(t *testing.T) {
	// Test-function text in the stderr tail ("...Login...") must never read as
	// codex auth. The process exited cleanly (code 0) with that apply_patch error
	// only as incidental tail output, so it classifies as an interrupted session —
	// the one thing it must NOT be is auth_required.
	detail := `acp process exited with code 0: process exited: ERROR codex_core::tools::router: error=apply_patch verification failed: Failed to find expected lines in /Users/wwcome/work/tutti-os/tutti/services/tuttid/service/agentstatus/service_test.go:
func TestServiceLoginRunsProviderLoginCommand(t *testing.T) {
	service := testService(func(name string) (string, error) {`
	if got := visibleFailureCode(detail); got == "auth_required" {
		t.Fatalf("visibleFailureCode() = auth_required, but embedded test text must not read as codex auth")
	}
}

func TestVisibleFailureCodeDoesNotTreatMcpServerAuthAsCodexAuth(t *testing.T) {
	// A Notion/Figma MCP server's expired OAuth token crashes codex's MCP client
	// (rmcp) and bubbles up here. It mentions "access token"/"AuthRequired", which
	// trips the auth pattern, but codex itself is still signed in — so this must
	// NOT surface as "Codex needs authentication". The exit is code 0 (a clean
	// shutdown), so it reads as an interrupted session, never auth_required.
	detail := `acp process exited with code 0: process exited: ERROR rmcp::transport::worker: ` +
		`worker quit with fatal: Transport channel closed, when AuthRequired(AuthRequiredError { ` +
		`www_authenticate_header: "Bearer realm=\"OAuth\", ` +
		`resource_metadata=\"https://mcp.notion.com/.well-known/oauth-protected-resource/mcp\", ` +
		`error=\"invalid_token\", error_description=\"Missing or invalid access token\"" })`
	if got := visibleFailureCode(detail); got == "auth_required" {
		t.Fatalf("visibleFailureCode() = auth_required, but MCP server auth must not read as codex auth")
	}
	if got := visibleFailureCode(detail); got != "session_interrupted" {
		t.Fatalf("visibleFailureCode() = %q, want session_interrupted (clean exit-0 MCP failure)", got)
	}
}

func TestVisibleFailureCodeClassifiesCleanExitAsInterrupted(t *testing.T) {
	// A clean exit (code 0) reaching here means the app-server was stopped
	// externally mid-turn (host quit, or an agent killed its own host) — the
	// session was interrupted, not "Codex request failed".
	for _, detail := range []string{
		"acp process exited with code 0: ",
		"acp process exited with code 0: shutting down",
	} {
		if got := visibleFailureCode(detail); got != "session_interrupted" {
			t.Fatalf("visibleFailureCode(%q) = %q, want session_interrupted", detail, got)
		}
	}
	if !visibleFailureRetryable("session_interrupted", "acp process exited with code 0: ") {
		t.Fatal("session_interrupted should be retryable")
	}
}

func TestVisibleFailureCodeClassifiesSignalKillAsInterrupted(t *testing.T) {
	// Signal-terminations (128+N: 137 SIGKILL, 143 SIGTERM, 130 SIGINT) are the
	// process being killed externally, not codex erroring out.
	for _, detail := range []string{
		"acp process exited with code 137: ",
		"acp process exited with code 143: ",
		"acp process exited with code 130: ",
	} {
		if got := visibleFailureCode(detail); got != "session_interrupted" {
			t.Fatalf("visibleFailureCode(%q) = %q, want session_interrupted", detail, got)
		}
	}
}

func TestVisibleFailureCodeClassifiesGoSignalExitAsInterrupted(t *testing.T) {
	// Regression: the claude-code sidecar's process wrapper
	// (localProcessConnection in process_transport.go) reports a
	// signal-terminated exit via Go's exec.ExitError.ExitCode(), which
	// returns -1 — not the 128+N convention codex's own app-server uses for
	// the same event. Seen in the field: tuttid's graceful-shutdown path
	// (CloseAllLiveSessions) sends SIGTERM to a live claude-code sidecar
	// mid-turn, and the resulting "exited with code -1" must read as a calm,
	// retryable interruption rather than "Claude Code request failed".
	for _, detail := range []string{
		"claude sdk sidecar exited with code -1",
		"claude sdk sidecar exited with code -1: ",
	} {
		if got := visibleFailureCode(detail); got != "session_interrupted" {
			t.Fatalf("visibleFailureCode(%q) = %q, want session_interrupted", detail, got)
		}
	}
	if !visibleFailureRetryable("session_interrupted", "claude sdk sidecar exited with code -1") {
		t.Fatal("session_interrupted should be retryable")
	}
}

func TestVisibleFailureCodeClassifiesUsageLimitAsQuota(t *testing.T) {
	// The most common real codex failure in the field is the ChatGPT usage cap,
	// delivered as plain text (no structured codexErrorInfo). It must read as a
	// quota/rate-limit, not a generic "request failed".
	detail := "You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), " +
		"visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again later."
	if got := visibleFailureCode(detail); got != "quota_or_rate_limit" {
		t.Fatalf("visibleFailureCode() = %q, want quota_or_rate_limit", got)
	}
	if got := visibleFailureCode("API Error: 403 Key limit exceeded (total limit)"); got != "quota_or_rate_limit" {
		t.Fatalf("visibleFailureCode() = %q, want quota_or_rate_limit", got)
	}
}

func TestVisibleFailureCodeClassifiesInsufficientCredits(t *testing.T) {
	for _, detail := range []string{
		`unexpected status 402 Payment Required: pre-deduct credits failed, url: https://llm-api.tutti.sh/v1/responses`,
		`unexpected status 402 Payment Required: {"error":{"message":"insufficient credits","type":"billing_error","code":"insufficient_credits"}}`,
		`Kimi API request failed: 402 Payment Required: OAuth credentials rejected`,
		`Provider request failed because the account balance is insufficient`,
		`You've hit your usage limit. Insufficient credits. View DinTalDock plans at https://tutti.sh/profile/plan, or try again later.`,
	} {
		if got := visibleFailureCode(detail); got != "insufficient_credits" {
			t.Fatalf("visibleFailureCode(%q) = %q, want insufficient_credits", detail, got)
		}
	}
	if visibleFailureRetryable("insufficient_credits", "402 Payment Required") {
		t.Fatal("insufficient_credits should not be retryable")
	}
}

func TestVisibleFailureCodeClassifiesSubscriptionAndQuotaBeforeAuthWrapper(t *testing.T) {
	tests := map[string]string{
		`Kimi Code models endpoint rejected OAuth credentials: error, status code: 402, message: We're unable to verify your membership benefits at this time. Please ensure your membership is active.`: "subscription_required",
		`Kimi Code models endpoint rejected the API key: error, status code: 401, message: Your current subscription does not have access to kimi-for-coding-highspeed.`:                                 "subscription_required",
		`Kimi Code request rejected OAuth credentials: error, status code: 403, message: You've reached your usage limit for this billing cycle.`:                                                        "quota_or_rate_limit",
	}
	for detail, want := range tests {
		if got := visibleFailureCode(detail); got != want {
			t.Fatalf("visibleFailureCode(%q) = %q, want %q", detail, got, want)
		}
	}
}

func TestVisibleFailureContentDescribesProviderInsufficientCredits(t *testing.T) {
	got := visibleFailureContent(ProviderTuttiAgent, "turn", "insufficient_credits")
	want := "DinTalDock Agent could not continue because the account has insufficient credits or balance."
	if got != want {
		t.Fatalf("visibleFailureContent() = %q, want %q", got, want)
	}
}

func TestVisibleFailureCodeDoesNotMisclassifyStructuredProviderFailuresAsAuth(t *testing.T) {
	tests := map[string]string{
		`HTTP 403: {"error":{"code":"model_not_allowed","message":"authorization denied for model"}}`:                     "model_not_allowed",
		`MCP client for codex_apps failed: HTTP 451: {"message":"no_biscuit_no_service"} authentication transport failed`: "plugin_unavailable",
		`HTTP 451: {"message":"no_biscuit_no_service"} authentication transport failed`:                                   "provider_error",
	}
	for detail, want := range tests {
		if got := visibleFailureCode(detail); got != want {
			t.Fatalf("visibleFailureCode(%q) = %q, want %q", detail, got, want)
		}
	}
}

func TestVisibleFailureContentDescribesInterruptedSession(t *testing.T) {
	got := visibleFailureContent(ProviderCodex, "turn", "session_interrupted")
	want := "Codex stopped unexpectedly before it finished responding. Try again."
	if got != want {
		t.Fatalf("visibleFailureContent() = %q, want %q", got, want)
	}
}

func TestVisibleFailureCodeStillClassifiesCodexOwnAuth(t *testing.T) {
	// Codex's own login failure must still be auth_required (guard against the MCP
	// exclusion being too broad).
	for _, detail := range []string{
		"acp process exited with code 1: process exited: not logged in. Please run /login.",
		"401 Unauthorized: invalid authentication credentials",
	} {
		if got := visibleFailureCode(detail); got != "auth_required" {
			t.Fatalf("visibleFailureCode(%q) = %q, want auth_required", detail, got)
		}
	}
}

func TestVisibleFailureCodeClassifiesMissingBinaryAsCliNotFound(t *testing.T) {
	// A run that can't find the CLI binary surfaces as an exec error; this is the
	// real "not installed / not on PATH" failure (the aspirational CODEX_CLI_MISSING
	// never reaches the run pipeline), so it must be distinct from a genuine exit.
	for _, detail := range []string{
		`fork/exec /Users/asdf/.local/bin/codex: no such file or directory`,
		`spawn codex ENOENT`,
		`codex: command not found`,
	} {
		if got := visibleFailureCode(detail); got != "cli_not_found" {
			t.Fatalf("visibleFailureCode(%q) = %q, want cli_not_found", detail, got)
		}
	}
}

func TestVisibleFailureCodeDoesNotClassifyMetadataReadAsMissingCLI(t *testing.T) {
	detail := `read claude system prompt: open /run/tsh/managed-agent/session/claude-system-prompt.md: no such file or directory`
	if got := visibleFailureCode(detail); got != "provider_error" {
		t.Fatalf("visibleFailureCode(%q) = %q, want provider_error", detail, got)
	}
}

func TestVisibleFailureCodeClassifiesGenuineExitAsProcessExited(t *testing.T) {
	// A non-zero exit that is NOT a missing binary stays process_exited.
	if got := visibleFailureCode("codex process exited with code 1"); got != "process_exited" {
		t.Fatalf("visibleFailureCode() = %q, want process_exited", got)
	}
}

func TestVisibleFailureCodeClassifiesExplicitLoginFailureAsAuth(t *testing.T) {
	if got := visibleFailureCode("Please login to continue."); got != "auth_required" {
		t.Fatalf("visibleFailureCode() = %q, want auth_required", got)
	}
}

func TestVisibleFailureCodeClassifiesVersionUnsupported(t *testing.T) {
	for _, detail := range []string{
		`codex-acp requires a newer version of codex`,
		`installed codex version is too old`,
	} {
		if got := visibleFailureCode(detail); got != "cli_version_unsupported" {
			t.Fatalf("visibleFailureCode(%q) = %q, want cli_version_unsupported", detail, got)
		}
	}
}

func TestVisibleFailureCodeClassifiesNetworkError(t *testing.T) {
	for _, detail := range []string{
		`request failed: getaddrinfo ENOTFOUND api.anthropic.com`,
		`connect ECONNREFUSED 127.0.0.1:443`,
		`Error: socket hang up`,
	} {
		if got := visibleFailureCode(detail); got != "network_error" {
			t.Fatalf("visibleFailureCode(%q) = %q, want network_error", detail, got)
		}
	}
}

func TestVisibleFailureCodeStreamDisconnectBeatsNetworkMarker(t *testing.T) {
	// A stream-disconnect detail can also mention "network error"; the more
	// specific stream classification must still win.
	detail := `stream disconnected before completion: Transport error: network error: error decoding response body`
	if got := visibleFailureCode(detail); got != "provider_stream_disconnected" {
		t.Fatalf("visibleFailureCode() = %q, want provider_stream_disconnected", got)
	}
}

func TestVisibleFailureRetryableForNetworkButNotMissingCli(t *testing.T) {
	if !visibleFailureRetryable("network_error", "ECONNRESET") {
		t.Fatal("network_error should be retryable")
	}
	if visibleFailureRetryable("cli_not_found", "ENOENT") {
		t.Fatal("cli_not_found should not be retryable")
	}
}

// The 2026-09-14 Demo failure: Codex registered a custom tool, the Responses to
// Chat gateway forwarded it unchanged, and the function-only upstream rejected
// the declaration. The observed error_json is reproduced verbatim here.
const demoProviderToolProtocolFailureDetail = "Failed to deserialize the JSON body into the target type: " +
	"tools[7].type: unknown variant `custom`, expected `function`"

func TestVisibleFailureCodeClassifiesProviderToolProtocolIncompatibility(t *testing.T) {
	for _, detail := range []string{
		demoProviderToolProtocolFailureDetail,
		`{"error":{"type":"invalid_request_error","message":"` + demoProviderToolProtocolFailureDetail + `"}}`,
		`unexpected status 400 Bad Request: invalid_request_error: tools.3.type: unsupported variant`,
		`invalid_request_error: tools[0].type: invalid tool type`,
	} {
		if got := visibleFailureCode(detail); got != FailureCodeProviderProtocolIncompatible {
			t.Fatalf(
				"visibleFailureCode(%q) = %q, want %q",
				detail,
				got,
				FailureCodeProviderProtocolIncompatible,
			)
		}
	}
}

func TestVisibleFailureCodeKeepsOtherInvalidRequestErrorsGeneric(t *testing.T) {
	// The classification must stay narrow: a plain invalid_request_error — or any
	// other declaration problem — is not a tool-protocol mismatch and keeps the
	// generic bucket.
	for _, detail := range []string{
		`invalid_request_error: missing required parameter: 'model'`,
		`invalid_request_error: messages[3].role: unknown variant ` + "`developer`" + `, expected ` + "`system`" + `, ` + "`user`",
		`invalid_request_error: tools[7] is not an object`,
		`tools[7].type: missing`,
		`400 Bad Request: no such tool type configured`,
	} {
		if got := visibleFailureCode(detail); got != "provider_error" {
			t.Fatalf("visibleFailureCode(%q) = %q, want provider_error", detail, got)
		}
	}
	// The classifications the new case sits between still win for their own
	// failures, so the narrow match cannot absorb account or auth errors.
	if got := visibleFailureCode("401 Unauthorized: invalid authentication credentials"); got != "auth_required" {
		t.Fatalf("visibleFailureCode() = %q, want auth_required", got)
	}
	if got := visibleFailureCode("API Error: 403 Key limit exceeded (total limit)"); got != "quota_or_rate_limit" {
		t.Fatalf("visibleFailureCode() = %q, want quota_or_rate_limit", got)
	}
	if got := visibleFailureCode("You've hit your usage limit. Upgrade to Pro"); got != "quota_or_rate_limit" {
		t.Fatalf("visibleFailureCode() = %q, want quota_or_rate_limit", got)
	}
}

func TestProviderToolProtocolIncompatibleRequiresToolSlotAndVerdict(t *testing.T) {
	// A numbered tool-declaration slot plus a rejection verdict is the whole
	// contract. Either half alone must not match.
	for _, detail := range []string{
		demoProviderToolProtocolFailureDetail,
		"tools[12].type: unsupported tool type",
		"tools.0.type: unknown variant",
		"tools[7].type: unknown variant",
	} {
		if !ProviderToolProtocolIncompatible(detail) {
			t.Fatalf("ProviderToolProtocolIncompatible(%q) = false, want true", detail)
		}
	}
	for _, detail := range []string{
		"unknown variant `custom`, expected `function`",
		"invalid_request_error",
		"tools: unknown variant",
		"tools[7]: unknown variant",
		"tools[7].name: unknown variant",
		"tools[] .type unknown variant",
		"",
	} {
		if ProviderToolProtocolIncompatible(detail) {
			t.Fatalf("ProviderToolProtocolIncompatible(%q) = true, want false", detail)
		}
	}
}

func TestVisibleFailureCodeDoesNotReadProtocolMismatchAsAuth(t *testing.T) {
	// A verbose upstream body can mention credentials while the actual cause is
	// the rejected tool declaration; the protocol code must win so the card never
	// offers a sign-in or reinstall call-to-action.
	detail := `invalid_request_error: tools[7].type: unknown variant ` + "`custom`" +
		`, expected ` + "`function`" + `; request included an access token for the upstream endpoint`
	if got := visibleFailureCode(detail); got != FailureCodeProviderProtocolIncompatible {
		t.Fatalf(
			"visibleFailureCode(%q) = %q, want %q",
			detail,
			got,
			FailureCodeProviderProtocolIncompatible,
		)
	}
}

func TestVisibleFailureContentDescribesProviderToolProtocolIncompatibility(t *testing.T) {
	turn := visibleFailureContent(ProviderCodex, "turn", FailureCodeProviderProtocolIncompatible)
	wantTurn := "Codex could not complete this request because the current model endpoint does not accept this tool protocol."
	if turn != wantTurn {
		t.Fatalf("visibleFailureContent() = %q, want %q", turn, wantTurn)
	}
	start := visibleFailureContent(ProviderCodex, "start", FailureCodeProviderProtocolIncompatible)
	wantStart := "Codex could not start because the current model endpoint does not accept this tool protocol."
	if start != wantStart {
		t.Fatalf("visibleFailureContent() = %q, want %q", start, wantStart)
	}
	if visibleFailureRetryable(FailureCodeProviderProtocolIncompatible, demoProviderToolProtocolFailureDetail) {
		t.Fatal("a tool-protocol mismatch must not be advertised as retryable")
	}
}

func TestProjectVisibleFailureMarksOnlyProtocolDetailExpandable(t *testing.T) {
	protocol, ok := projectVisibleFailure(canonical.EventSource{}, failedTurnEvent(
		"event-protocol",
		demoProviderToolProtocolFailureDetail,
	))
	if !ok {
		t.Fatal("protocol failure projection was not produced")
	}
	if protocol.payload["code"] != FailureCodeProviderProtocolIncompatible {
		t.Fatalf("projected code = %v, want %q", protocol.payload["code"], FailureCodeProviderProtocolIncompatible)
	}
	if protocol.payload["detailAvailable"] != true {
		t.Fatalf("projected detailAvailable = %v, want true", protocol.payload["detailAvailable"])
	}
	if protocol.payload["detail"] != demoProviderToolProtocolFailureDetail {
		t.Fatalf("projected detail = %v, want the raw upstream text", protocol.payload["detail"])
	}

	// Every other code keeps the card it had: the raw text stays in the payload
	// for the canonical model, but the card does not invite expanding it.
	other, ok := projectVisibleFailure(canonical.EventSource{}, failedTurnEvent(
		"event-auth",
		"401 Unauthorized: invalid authentication credentials",
	))
	if !ok {
		t.Fatal("auth failure projection was not produced")
	}
	if other.payload["code"] != "auth_required" {
		t.Fatalf("projected code = %v, want auth_required", other.payload["code"])
	}
	if _, present := other.payload["detailAvailable"]; present {
		t.Fatalf("projected detailAvailable = %v, want absent", other.payload["detailAvailable"])
	}
}

func TestProjectStoredTurnErrorRefinesLegacyProtocolFailure(t *testing.T) {
	code, detail := ProjectStoredTurnError("provider_error", demoProviderToolProtocolFailureDetail)
	if code != FailureCodeProviderProtocolIncompatible {
		t.Fatalf("ProjectStoredTurnError() code = %q, want %q", code, FailureCodeProviderProtocolIncompatible)
	}
	if detail != demoProviderToolProtocolFailureDetail {
		t.Fatalf("ProjectStoredTurnError() detail = %q, want the stored raw text", detail)
	}

	// A stored row written by the fixed runtime already carries the narrow code;
	// the read-side projection must still hand back the raw detail.
	code, detail = ProjectStoredTurnError(FailureCodeProviderProtocolIncompatible, demoProviderToolProtocolFailureDetail)
	if code != FailureCodeProviderProtocolIncompatible || detail != demoProviderToolProtocolFailureDetail {
		t.Fatalf("ProjectStoredTurnError() = (%q, %q), want the narrow code with its detail", code, detail)
	}
}

func TestProjectStoredTurnErrorPreservesUnrelatedErrors(t *testing.T) {
	assertPreserved := func(code string, message string) {
		t.Helper()
		gotCode, gotDetail := ProjectStoredTurnError(code, message)
		if gotCode != code {
			t.Fatalf(
				"ProjectStoredTurnError(%q, %q) code = %q, want the stored code unchanged",
				code,
				message,
				gotCode,
			)
		}
		if gotDetail != "" {
			t.Fatalf(
				"ProjectStoredTurnError(%q, %q) detail = %q, want empty",
				code,
				message,
				gotDetail,
			)
		}
	}
	assertPreserved("", "codex process exited with code 1")
	assertPreserved("cli_not_found", "spawn codex ENOENT")
	assertPreserved("auth_required", demoProviderToolProtocolFailureDetail)
	assertPreserved("auth_required", "401 Unauthorized: invalid authentication credentials")
	assertPreserved("quota_or_rate_limit", "You've hit your usage limit")
}

func TestProjectStoredTurnErrorCapsDetailAtPublishedLimit(t *testing.T) {
	message := demoProviderToolProtocolFailureDetail + strings.Repeat(" y", 400)
	_, detail := ProjectStoredTurnError("provider_error", message)
	if len(detail) > CanonicalTurnErrorDetailLimit {
		t.Fatalf(
			"detail length = %d, want at most %d",
			len(detail),
			CanonicalTurnErrorDetailLimit,
		)
	}
	if !strings.HasPrefix(detail, "Failed to deserialize the JSON body into the target type") {
		t.Fatalf("detail = %q, want the beginning of the stored message", detail)
	}
	if !strings.HasSuffix(detail, "...") {
		t.Fatalf("detail = %q, want an explicit truncation marker", detail)
	}
}

func TestLimitCanonicalTurnErrorDetailCutsOnARuneBoundary(t *testing.T) {
	// The byte limit lands two bytes inside a three-byte rune, so a plain byte
	// slice would put invalid UTF-8 into the transport projection.
	value := strings.Repeat("a", 100) + strings.Repeat("中", 100)
	got := limitCanonicalTurnErrorDetail(value)
	if len(got) > CanonicalTurnErrorDetailLimit {
		t.Fatalf(
			"detail length = %d, want at most %d",
			len(got),
			CanonicalTurnErrorDetailLimit,
		)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("detail is not valid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 100)) {
		t.Fatalf("detail = %q, want the text that fits before the cut", got)
	}
}

func failedTurnEvent(eventID string, detail string) activityshared.Event {
	return activityshared.Event{
		EventID:  eventID,
		Type:     activityshared.EventTurnFailed,
		Provider: activityshared.Provider(ProviderCodex),
		Payload: activityshared.EventPayload{
			TurnID:   "turn-1",
			Metadata: map[string]any{"error": detail},
		},
	}
}

func reportTestSource() canonical.EventSource {
	return canonical.EventSource{Provider: ProviderClaudeCode}
}
