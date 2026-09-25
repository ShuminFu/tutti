package agentruntime

import (
	"errors"
	"testing"
)

func TestIsACPProviderSessionNotFoundAcceptsResourceSuffixDetails(t *testing.T) {
	t.Parallel()

	callErr := &acpCallError{
		Method: acpMethodLoadSession,
		Err: acpError{
			Code:    -32002,
			Message: "Resource not found: a4009694-9d5c-48be-8480-6d1e0ede5410",
		},
	}

	if !isACPProviderSessionNotFound(acpMethodLoadSession, callErr) {
		t.Fatalf("expected load-session missing resource error with suffix details to be classified as provider session missing")
	}
}

func TestIsACPProviderSessionNotFoundAcceptsCodexAppServerMissingRollout(t *testing.T) {
	t.Parallel()

	// Codex app server thread/resume reports an absent rollout file as -32600
	// "no rollout found for thread id …" (imported conversation whose rollout is
	// not on this device). It must be classified so the runtime recreates the
	// provider session instead of dead-ending the user.
	callErr := &acpCallError{
		Method: appServerMethodThreadResume,
		Err: acpError{
			Code:    -32600,
			Message: "no rollout found for thread id 019eeec5-940b-7c00-9fef-a0da2990cfe5",
		},
	}

	if !isACPProviderSessionNotFound(appServerMethodThreadResume, callErr) {
		t.Fatalf("expected codex app server missing-rollout thread/resume error to be classified as provider session missing")
	}
}

func TestClassifyACPResumeErrorMapsMissingRolloutToProviderSessionNotFound(t *testing.T) {
	t.Parallel()

	callErr := &acpCallError{
		Method: appServerMethodThreadResume,
		Err: acpError{
			Code:    -32600,
			Message: "no rollout found for thread id 019eeec5-940b-7c00-9fef-a0da2990cfe5",
		},
	}

	classified := classifyACPResumeError(
		Session{Provider: ProviderCodex, ProviderSessionID: "019eeec5-940b-7c00-9fef-a0da2990cfe5"},
		appServerMethodThreadResume,
		callErr,
	)
	if AppErrorCode(classified) != AppErrorProviderSessionNotFound {
		t.Fatalf("classified error code = %q, want %q", AppErrorCode(classified), AppErrorProviderSessionNotFound)
	}
	// The classified error must be recreatable so RecreateIfMissing can recover.
	if !isResumeRecreatableError(classified) {
		t.Fatalf("missing-rollout error should be recreatable")
	}
}

func TestClassifyACPResumeErrorMapsActiveWriterToHeldExternally(t *testing.T) {
	t.Parallel()

	callErr := &acpCallError{
		Method: appServerMethodThreadResume,
		Err: acpError{
			Code:    -32600,
			Message: "thread 019eeec5-940b-7c00-9fef-a0da2990cfe5 already has an active writer",
		},
	}
	session := Session{
		Provider:          ProviderCodex,
		ProviderSessionID: "019eeec5-940b-7c00-9fef-a0da2990cfe5",
		Env:               []string{"TUTTI_CODEX_CONFIG_OVERRIDES=[\"sandbox_mode=\\\"workspace-write\\\"\"]"},
	}
	classified := classifyACPResumeError(session, appServerMethodThreadResume, callErr)
	if AppErrorCode(classified) != AppErrorCodexThreadHeldExternally {
		t.Fatalf("classified error code = %q, want %q", AppErrorCode(classified), AppErrorCodexThreadHeldExternally)
	}
	if !errors.Is(classified, ErrCodexThreadHeldExternally) {
		t.Fatalf("classified error = %v, want ErrCodexThreadHeldExternally", classified)
	}
	if isResumeRecreatableError(classified) {
		t.Fatal("an externally held thread must not be recreated")
	}
	if isACPProviderSessionNotFound(appServerMethodThreadResume, callErr) {
		t.Fatal("active writer must not be classified as a missing rollout")
	}
}

func TestClassifyACPResumeErrorLeavesIsolatedWriterLockGeneric(t *testing.T) {
	t.Parallel()

	callErr := &acpCallError{
		Method: appServerMethodThreadResume,
		Err: acpError{
			Code:    -32600,
			Message: "thread abc already has an active writer",
		},
	}
	classified := classifyACPResumeError(
		Session{Provider: ProviderCodex, ProviderSessionID: "abc"},
		appServerMethodThreadResume,
		callErr,
	)
	if AppErrorCode(classified) != "" {
		t.Fatalf("isolated resume error code = %q, want the raw ACP error", AppErrorCode(classified))
	}
	if errors.Is(classified, ErrCodexThreadHeldExternally) {
		t.Fatal("isolated mode must not take the user-home hold path")
	}
}

func TestIsACPProviderSessionNotFoundIgnoresUnrelatedInvalidRequest(t *testing.T) {
	t.Parallel()

	// A generic -32600 that is NOT a missing rollout must not be misclassified.
	callErr := &acpCallError{
		Method: appServerMethodThreadResume,
		Err: acpError{
			Code:    -32600,
			Message: "invalid request: missing params",
		},
	}

	if isACPProviderSessionNotFound(appServerMethodThreadResume, callErr) {
		t.Fatalf("unrelated invalid-request error must not be classified as provider session missing")
	}
}
