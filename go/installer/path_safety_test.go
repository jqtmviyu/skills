package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSkillSanitizesDirectoryNameBeforeWriting(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	writeSkillSource(t, source, "confined\n")

	result, err := InstallSkill(Skill{Name: "../../Escape Folder", Path: source}, "codex", Options{CWD: project, Mode: Copy})
	if err != nil {
		t.Fatalf("install sanitized skill: %v", err)
	}
	want := filepath.Join(project, ".agents", "skills", "escape-folder")
	if !result.Success || result.Path != want {
		t.Fatalf("sanitized install path = %q, want %q", result.Path, want)
	}
	if _, err := os.Stat(filepath.Join(project, "Escape Folder")); !os.IsNotExist(err) {
		t.Fatalf("skill escaped its installation directory: %v", err)
	}
}

func TestInstallSkillSkipsWhenCanonicalDirectoryOverlapsSource(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, ".agents", "skills", "existing")
	writeSkillSource(t, source, "preserve canonical source\n")
	if err := os.WriteFile(filepath.Join(source, "support.txt"), []byte("preserve support\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkill(Skill{Name: "existing", Path: source}, "claude-code", Options{CWD: project})
	if err != nil {
		t.Fatalf("install source already in canonical dir: %v", err)
	}
	if !result.Success || !result.Skipped || result.Path != source || result.CanonicalPath != source {
		t.Fatalf("canonical-overlap result = %#v", result)
	}
	if contents, err := os.ReadFile(filepath.Join(source, "support.txt")); err != nil || string(contents) != "preserve support\n" {
		t.Fatalf("canonical source contents = %q, error %v", contents, err)
	}
}

func TestInstallSkillSkipsWhenDestinationIsNestedInsideSource(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "SKILL.md"), []byte("root skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "support.txt"), []byte("preserve root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkill(Skill{Name: "project-root", Path: project}, "codex", Options{CWD: project})
	if err != nil {
		t.Fatalf("install project-root skill: %v", err)
	}
	if !result.Success || !result.Skipped {
		t.Fatalf("nested-destination result = %#v", result)
	}
	if contents, err := os.ReadFile(filepath.Join(project, "support.txt")); err != nil || string(contents) != "preserve root\n" {
		t.Fatalf("project source was changed: %q, error %v", contents, err)
	}
}

func TestGlobalAgentSpecificInstallUsesIsolatedHomeAndOverride(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	customClaude := filepath.Join(root, "claude-config")
	source := filepath.Join(root, "source")
	for _, path := range []string{home, project, customClaude} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", customClaude)
	writeSkillSource(t, source, "global agent-specific\n")

	result, err := InstallSkill(Skill{Name: "global-claude", Path: source}, "claude-code", Options{Global: true, CWD: project})
	if err != nil {
		t.Fatalf("global agent-specific install: %v", err)
	}
	canonical := filepath.Join(home, ".agents", "skills", "global-claude")
	agentPath := filepath.Join(customClaude, "skills", "global-claude")
	if !result.Success || result.Path != agentPath || result.CanonicalPath != canonical {
		t.Fatalf("global agent-specific paths = %#v, want %q / %q", result, agentPath, canonical)
	}
	assertAgentLinkOrFallback(t, agentPath, result.SymlinkFailed)
}
