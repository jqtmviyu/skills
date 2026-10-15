package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSkillCopiesToCanonicalAndLinksTheAgentDirectory(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := InstallSkill(Skill{Name: "Review Skill", Path: source, PluginName: "review-tools"}, "claude-code", Options{CWD: project})
	if err != nil {
		t.Fatalf("install skill: %v", err)
	}
	canonical := filepath.Join(project, ".agents", "skills", "review-skill")
	agentPath := filepath.Join(project, ".claude", "skills", "review-skill")
	if !result.Success {
		t.Fatalf("install result = %#v", result)
	}
	if result.Path != agentPath || result.CanonicalPath != canonical {
		t.Fatalf("install paths = %q / %q, want %q / %q", result.Path, result.CanonicalPath, agentPath, canonical)
	}
	if result.PluginName != "review-tools" {
		t.Fatalf("plugin metadata = %q, want review-tools", result.PluginName)
	}
	linkInfo, err := os.Lstat(agentPath)
	if err != nil {
		t.Fatalf("inspect agent install: %v", err)
	}
	if result.SymlinkFailed {
		if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.IsDir() {
			t.Fatalf("fallback agent install mode = %v, want copied directory", linkInfo.Mode())
		}
	} else if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("agent install mode = %v, want symlink", linkInfo.Mode())
	}
	if contents, err := os.ReadFile(filepath.Join(agentPath, "SKILL.md")); err != nil || string(contents) != "skill content\n" {
		t.Fatalf("agent skill contents = %q, error %v", contents, err)
	}
}

func assertAgentLinkOrFallback(t *testing.T, path string, symlinkFailed bool) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("inspect agent installation %s: %v", path, err)
	}
	if symlinkFailed {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			t.Fatalf("fallback installation mode = %v, want copied directory", info.Mode())
		}
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("agent installation mode = %v, want symlink", info.Mode())
	}
}
