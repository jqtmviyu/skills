package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicInstallPathHelpersFollowResolvedAgentTargets(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	custom := filepath.Join(t.TempDir(), "claude")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", custom)

	projectPath, err := GetInstallPath("My Skill", "claude-code", Options{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, ".claude", "skills", "my-skill"); projectPath != want {
		t.Errorf("project install path = %q, want %q", projectPath, want)
	}
	globalPath, err := GetInstallPath("My Skill", "claude-code", Options{Global: true, CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(custom, "skills", "my-skill"); globalPath != want {
		t.Errorf("global install path = %q, want %q", globalPath, want)
	}
	canonical, err := GetCanonicalPath("My Skill", "claude-code", Options{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, ".agents", "skills", "my-skill"); canonical != want {
		t.Errorf("canonical path = %q, want %q", canonical, want)
	}
	eve, err := GetCanonicalPath("My Skill", "eve", Options{CWD: project, EveSubagent: "Research"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "agent", "subagents", "research", "skills", "my-skill"); eve != want {
		t.Errorf("Eve canonical path = %q, want %q", eve, want)
	}
}

func TestPublicIsSkillInstalledChecksResolvedDirectory(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".agents", "skills", "present")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsSkillInstalled("present", "codex", Options{CWD: project}) {
		t.Fatal("existing skill was not found")
	}
	if IsSkillInstalled("absent", "codex", Options{CWD: project}) {
		t.Fatal("missing skill was found")
	}
	if IsSkillInstalled("present", "eve", Options{Global: true, CWD: project}) {
		t.Fatal("skill found for agent that does not support global installs")
	}
}
