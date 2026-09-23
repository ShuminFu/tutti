package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCursorSkillImportRoutesPreviewAndCopy(t *testing.T) {
	project := t.TempDir()
	skillDir := filepath.Join(project, ".cursor", "skills", "example")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: example\ndescription: Example\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, NewRoutes(DaemonAPI{}))
	post := func(path string, input any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}
	preview := post("/v1/skills/cursor-import/preview", map[string]any{"sourceDir": filepath.Join(project, ".cursor")})
	if preview.Code != http.StatusOK || !bytes.Contains(preview.Body.Bytes(), []byte(`"status":"ready"`)) {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	imported := post("/v1/skills/cursor-import/import", map[string]any{"sourceDir": filepath.Join(project, ".cursor"), "names": []string{"example"}})
	if imported.Code != http.StatusOK || !bytes.Contains(imported.Body.Bytes(), []byte(`"status":"imported"`)) {
		t.Fatalf("import: %d %s", imported.Code, imported.Body.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "example", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
