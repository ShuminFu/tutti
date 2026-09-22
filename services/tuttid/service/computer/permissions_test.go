package computer

import (
	"reflect"
	"testing"
)

func TestParseComputerPermissionStatus(t *testing.T) {
	status, err := parseComputerPermissionStatus([]byte(`{
		"accessibility": true,
		"screen_recording": true,
		"screen_recording_capturable": true
	}`))
	if err != nil {
		t.Fatalf("parseComputerPermissionStatus returned error: %v", err)
	}
	if issues := computerPermissionIssues(status); len(issues) != 0 {
		t.Fatalf("computerPermissionIssues = %v, want none", issues)
	}
}

func TestParseComputerPermissionStatusToleratesDiagnosticOutput(t *testing.T) {
	status, err := parseComputerPermissionStatus([]byte(`cua-driver diagnostic
{
	"accessibility": true,
	"screen_recording": false,
	"screen_recording_capturable": true
}`))
	if err != nil {
		t.Fatalf("parseComputerPermissionStatus returned error: %v", err)
	}
	issues := computerPermissionIssues(status)
	if !reflect.DeepEqual(issues, []string{"missing Screen Recording"}) {
		t.Fatalf("computerPermissionIssues = %v, want missing Screen Recording", issues)
	}
}

func TestComputerPermissionIssuesRequiresBothPermissions(t *testing.T) {
	status := computerPermissionStatus{
		Accessibility:             boolPtr(false),
		ScreenRecording:           boolPtr(true),
		ScreenRecordingCapturable: boolPtr(false),
	}
	issues := computerPermissionIssues(status)
	want := []string{
		"missing Accessibility",
		"Screen Recording authorized but not capturable; restart CuaDriver and check again",
	}
	if !reflect.DeepEqual(issues, want) {
		t.Fatalf("computerPermissionIssues = %v, want %v", issues, want)
	}
}

func TestParseWindowsDriverDoctor(t *testing.T) {
	doctor, err := parseWindowsDriverDoctor([]byte("driver diagnostic\n{\"ok\":true,\"probes\":[{\"label\":\"UI Automation\",\"status\":\"ok\"}]}"))
	if err != nil {
		t.Fatalf("parseWindowsDriverDoctor: %v", err)
	}
	if !doctor.OK {
		t.Fatalf("doctor = %#v, want ok", doctor)
	}
	if _, err := parseWindowsDriverDoctor([]byte(`{"ok":false}`)); err != nil {
		t.Fatalf("parseWindowsDriverDoctor false result: %v", err)
	}
}

func TestReadOnlyCaptureStatusWithGrantReceipt(t *testing.T) {
	for _, tc := range []struct {
		name, capture, state, receipt string
		allowed                       bool
	}{
		{"verified grant", "null", "not_checked", `{"bundle_id":"com.trycua.driver","source":"permissions_grant","verified_at":"2026-09-20T09:36:45Z"}`, true},
		{"no receipt", "null", "not_checked", `null`, false},
		{"explicit capture failure", "false", "not_checked", `{"bundle_id":"com.trycua.driver","source":"permissions_grant","verified_at":"2026-09-20T09:36:45Z"}`, false},
		{"failed status", "null", "failed", `{"bundle_id":"com.trycua.driver","source":"permissions_grant","verified_at":"2026-09-20T09:36:45Z"}`, false},
		{"different identity", "null", "not_checked", `{"bundle_id":"other.app","source":"permissions_grant","verified_at":"2026-09-20T09:36:45Z"}`, false},
		{"different source", "null", "not_checked", `{"bundle_id":"com.trycua.driver","source":"other","verified_at":"2026-09-20T09:36:45Z"}`, false},
		{"invalid receipt", "null", "not_checked", `{"bundle_id":"com.trycua.driver","source":"permissions_grant","verified_at":""}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, err := parseComputerPermissionStatus([]byte(`{"accessibility":true,"screen_recording":true,"screen_recording_capturable":` + tc.capture + `,"direct_capture_status":"` + tc.state + `","direct_capture_verification":` + tc.receipt + `}`))
			if err != nil {
				t.Fatal(err)
			}
			if allowed := len(computerPermissionIssues(status)) == 0; allowed != tc.allowed {
				t.Fatalf("allowed = %v, want %v", allowed, tc.allowed)
			}
			status.Accessibility = boolPtr(false)
			if len(computerPermissionIssues(status)) == 0 {
				t.Fatal("grant receipt must not override revoked Accessibility")
			}
			status.Accessibility = boolPtr(true)
			status.ScreenRecording = boolPtr(false)
			if len(computerPermissionIssues(status)) == 0 {
				t.Fatal("grant receipt must not override revoked Screen Recording")
			}
		})
	}
}

func boolPtr(value bool) *bool {
	return &value
}
