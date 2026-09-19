package localskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadParsesSkillMetadata(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: my-skill\ndescription: A skill.\n---\n\nBody.\n")

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Name != "my-skill" || skill.Description != "A skill." {
		t.Fatalf("skill = %+v, want the frontmatter values", skill)
	}
	if skill.Path != filepath.Clean(path) {
		t.Fatalf("path = %q, want %q", skill.Path, filepath.Clean(path))
	}
	if !skill.Automatic || !skill.UserInvocable {
		t.Fatalf("skill = %+v, want both flags to default to true", skill)
	}
	if skill.SourceKind != "" || skill.PluginName != "" {
		t.Fatalf("skill = %+v, want root facts left empty by Read", skill)
	}
}

func TestReadAcceptsBOMAndCRLF(t *testing.T) {
	dir := t.TempDir()
	content := utf8BOM + "---\r\nname: bom-skill\r\ndescription: Windows authored.\r\n---\r\n\r\nBody.\r\n"
	path := writeFile(t, filepath.Join(dir, "SKILL.md"), content)

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Name != "bom-skill" || skill.Description != "Windows authored." {
		t.Fatalf("skill = %+v, want the Unix-normalized metadata", skill)
	}
}

func TestReadParsesBlockScalarDescriptions(t *testing.T) {
	cases := []struct {
		name   string
		scalar string
		want   string
	}{
		{name: "folded", scalar: ">-\n    first line\n    second line", want: "first line second line"},
		{name: "literal", scalar: "|\n    first line\n    second line", want: "first line\nsecond line"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, filepath.Join(dir, "SKILL.md"),
				"---\nname: block-skill\ndescription: "+testCase.scalar+"\n---\n\nBody.\n")
			skill, err := Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if skill.Description != testCase.want {
				t.Fatalf("description = %q, want %q", skill.Description, testCase.want)
			}
		})
	}
}

func TestReadAcceptsUnicodeSkillNames(t *testing.T) {
	dir := t.TempDir()
	path := writeValidSkill(t, dir, "技能名称", "Chinese name.")

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Name != "技能名称" {
		t.Fatalf("name = %q, want the Chinese name", skill.Name)
	}
}

func TestReadAcceptsEveryAllowedNameCharacter(t *testing.T) {
	for _, name := range []string{"a", "a1_b.c-d", "A.B_C-9", strings.Repeat("a", 64)} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeValidSkill(t, dir, name, "Allowed name.")
			skill, err := Read(path)
			if err != nil {
				t.Fatalf("Read(%q): %v", name, err)
			}
			if skill.Name != name {
				t.Fatalf("name = %q, want %q", skill.Name, name)
			}
		})
	}
	// A name may start with a digit, but a bare number is a YAML integer, so
	// it has to be quoted to still be a string.
	t.Run("quoted digit first", func(t *testing.T) {
		path := writeFile(t, filepath.Join(t.TempDir(), "SKILL.md"),
			"---\nname: \"1\"\ndescription: Digit first.\n---\n")
		skill, err := Read(path)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if skill.Name != "1" {
			t.Fatalf("name = %q, want 1", skill.Name)
		}
	})
}

func TestReadRejectsInvalidMetadata(t *testing.T) {
	longName := strings.Repeat("a", 65)
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty file", content: "", want: "missing --- frontmatter delimiter"},
		{name: "no frontmatter", content: "# Title\n\nBody.\n", want: "missing --- frontmatter delimiter"},
		{name: "unterminated frontmatter", content: "---\nname: ok\ndescription: ok\n", want: "unterminated --- frontmatter"},
		{name: "empty frontmatter", content: "---\n---\n", want: "frontmatter is empty"},
		{name: "frontmatter is not a mapping", content: "---\n- one\n- two\n---\n", want: "must be a YAML mapping"},
		{name: "invalid YAML", content: "---\nname: [unclosed\ndescription: ok\n---\n", want: "invalid frontmatter YAML"},
		{name: "duplicate key", content: "---\nname: one\nname: two\ndescription: ok\n---\n", want: `duplicate frontmatter key "name"`},
		{name: "missing name", content: "---\ndescription: ok\n---\n", want: `frontmatter "name" is required`},
		{name: "missing description", content: "---\nname: ok\n---\n", want: `frontmatter "description" is required`},
		{name: "null name", content: "---\nname:\ndescription: ok\n---\n", want: `frontmatter "name" must be a string`},
		{name: "numeric name", content: "---\nname: 12\ndescription: ok\n---\n", want: `frontmatter "name" must be a string`},
		{name: "mapping name", content: "---\nname:\n  nested: true\ndescription: ok\n---\n", want: `frontmatter "name" must be a string`},
		{name: "blank description", content: "---\nname: ok\ndescription: \"   \"\n---\n", want: `frontmatter "description" must not be empty`},
		{name: "name with slash", content: "---\nname: bad/name\ndescription: ok\n---\n", want: "must start with a letter or digit"},
		{name: "name with space", content: "---\nname: bad name\ndescription: ok\n---\n", want: "must start with a letter or digit"},
		{name: "name with leading hyphen", content: "---\nname: -bad\ndescription: ok\n---\n", want: "must start with a letter or digit"},
		{name: "name too long", content: "---\nname: " + longName + "\ndescription: ok\n---\n", want: "longer than 64 characters"},
		{name: "name with control character", content: "---\nname: \"bad\\tx\"\ndescription: ok\n---\n", want: "control character"},
		{name: "non boolean disable-model-invocation", content: "---\nname: ok\ndescription: ok\ndisable-model-invocation: \"true\"\n---\n", want: `frontmatter "disable-model-invocation" must be a boolean`},
		{name: "non boolean user-invocable", content: "---\nname: ok\ndescription: ok\nuser-invocable: \"false\"\n---\n", want: `frontmatter "user-invocable" must be a boolean`},
		{name: "oversized frontmatter", content: "---\nname: ok\ndescription: ok\ncomment: " + strings.Repeat("x", 70<<10) + "\n---\n", want: "frontmatter exceeds"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFile(t, filepath.Join(t.TempDir(), "SKILL.md"), testCase.content)
			_, err := Read(path)
			if err == nil {
				t.Fatalf("Read(%s) succeeded, want %q", path, testCase.want)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), testCase.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %q, want it to name %q", err.Error(), path)
			}
		})
	}
}

func TestReadRejectsEmptyPath(t *testing.T) {
	if _, err := Read("   "); err == nil {
		t.Fatal("Read(\"   \") succeeded, want an error")
	}
}

func TestReadReportsUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "SKILL.md")
	err := error(nil)
	if _, err = Read(path); err == nil {
		t.Fatal("Read succeeded, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %q, want it to name %q", err.Error(), path)
	}
}

func TestReadAutomaticFlags(t *testing.T) {
	cases := []struct {
		name        string
		frontmatter []string
		sidecar     string
		want        bool
	}{
		{name: "defaults to automatic", frontmatter: []string{"name: ok", "description: ok"}, want: true},
		{name: "disable-model-invocation", frontmatter: []string{"name: ok", "description: ok", "disable-model-invocation: true"}, want: false},
		{name: "disable-model-invocation false", frontmatter: []string{"name: ok", "description: ok", "disable-model-invocation: false"}, want: true},
		{name: "openai policy allows", sidecar: "policy:\n  allow_implicit_invocation: true\n", want: true},
		{name: "openai policy forbids", sidecar: "policy:\n  allow_implicit_invocation: false\n", want: false},
		{name: "openai policy absent", sidecar: "interface:\n  display_name: Skill\n", want: true},
		{name: "openai policy empty file", sidecar: "\n\n", want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			frontmatter := testCase.frontmatter
			if len(frontmatter) == 0 {
				frontmatter = []string{"name: ok", "description: ok"}
			}
			path := writeSkill(t, dir, frontmatter...)
			if testCase.sidecar != "" {
				writeFile(t, filepath.Join(dir, "agents", "openai.yaml"), testCase.sidecar)
			}
			skill, err := Read(path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if skill.Automatic != testCase.want {
				t.Fatalf("Automatic = %v, want %v", skill.Automatic, testCase.want)
			}
			if !skill.UserInvocable {
				t.Fatalf("UserInvocable = false, want the sidecar to leave it alone")
			}
		})
	}
}

func TestReadUserInvocableFlag(t *testing.T) {
	dir := t.TempDir()
	path := writeSkill(t, dir, "name: ok", "description: ok", "user-invocable: false")

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.UserInvocable {
		t.Fatal("UserInvocable = true, want false")
	}
	if !skill.Automatic {
		t.Fatal("Automatic = false, want the skip to leave implicit invocation alone")
	}
}

func TestReadReportsInvalidOpenAIPolicy(t *testing.T) {
	cases := []struct {
		name    string
		sidecar string
		want    string
	}{
		{name: "invalid YAML", sidecar: "policy: [unclosed\n", want: "invalid YAML"},
		{name: "policy not a mapping", sidecar: "policy: nope\n", want: "policy must be a YAML mapping"},
		{name: "policy flag not a boolean", sidecar: "policy:\n  allow_implicit_invocation: \"false\"\n", want: "allow_implicit_invocation must be a boolean"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeValidSkill(t, dir, "ok", "ok")
			sidecar := writeFile(t, filepath.Join(dir, "agents", "openai.yaml"), testCase.sidecar)

			_, err := Read(path)
			if err == nil {
				t.Fatal("Read succeeded, want an error")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), testCase.want)
			}
			if !strings.Contains(err.Error(), sidecar) {
				t.Fatalf("error = %q, want it to name %q", err.Error(), sidecar)
			}
		})
	}
}

func TestReadBoundsLargeBodies(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "SKILL.md"),
		"---\nname: big\ndescription: Big body.\n---\n\n"+strings.Repeat("body line\n", 200<<10))

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Name != "big" {
		t.Fatalf("name = %q, want big", skill.Name)
	}
}

func TestReadDoesNotRequireBodyLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: no-body\ndescription: Frontmatter only.\n---\n")

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Name != "no-body" {
		t.Fatalf("name = %q, want no-body", skill.Name)
	}
}

func TestReadIsNotCached(t *testing.T) {
	dir := t.TempDir()
	path := writeValidSkill(t, dir, "hot", "First.")
	first, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if first.Description != "First." {
		t.Fatalf("description = %q, want First.", first.Description)
	}

	writeValidSkill(t, dir, "hot", "Second.")
	second, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if second.Description != "Second." {
		t.Fatalf("description = %q, want Second.", second.Description)
	}
}

func TestReadHandlesPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my project", "skill dir")
	path := writeValidSkill(t, dir, "spaced", "Spaces everywhere.")

	skill, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if skill.Path != filepath.Clean(path) {
		t.Fatalf("path = %q, want %q", skill.Path, filepath.Clean(path))
	}
	if _, err := os.Stat(skill.Path); err != nil {
		t.Fatalf("stat %q: %v", skill.Path, err)
	}
}
