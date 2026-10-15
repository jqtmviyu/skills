package installer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vercel-labs/skills/go/agent"
	"gopkg.in/yaml.v3"
)

func TestInstallSkillFallsBackToCopyWhenSymlinkCreationFails(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillSource(t, source, "fallback content\n")

	result, err := InstallSkill(Skill{Name: "fallback", Path: source}, "claude-code", Options{
		CWD:           project,
		CreateSymlink: func(string, string) error { return errors.New("symlink disabled") },
	})
	if err != nil {
		t.Fatalf("install with copy fallback: %v", err)
	}
	agentPath := filepath.Join(project, ".claude", "skills", "fallback")
	if !result.Success || !result.SymlinkFailed || result.Path != agentPath {
		t.Fatalf("fallback result = %#v", result)
	}
	info, err := os.Lstat(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("fallback target mode = %v, want copied directory", info.Mode())
	}
	if contents, err := os.ReadFile(filepath.Join(agentPath, "SKILL.md")); err != nil || string(contents) != "fallback content\n" {
		t.Fatalf("fallback contents = %q, error %v", contents, err)
	}
}

func TestInstallSkillSkipsMissingAgentProjectRootButHonorsExplicitAndClaudeTargets(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillSource(t, source, "available through canonical\n")

	result, err := InstallSkill(Skill{Name: "windsurf-skill", Path: source}, "windsurf", Options{CWD: project})
	if err != nil {
		t.Fatalf("install for unused agent: %v", err)
	}
	canonical := filepath.Join(project, ".agents", "skills", "windsurf-skill")
	if !result.Success || !result.Skipped || result.SkipReason != agent.MissingAgentProjectDirectory || result.Path != canonical {
		t.Fatalf("missing-root result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(project, ".windsurf")); !os.IsNotExist(err) {
		t.Fatalf("missing Windsurf root was created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
		t.Fatalf("canonical skill is unavailable: %v", err)
	}

	selectedProject := filepath.Join(root, "selected-project")
	if err := os.MkdirAll(selectedProject, 0o755); err != nil {
		t.Fatal(err)
	}
	selected, err := InstallSkill(Skill{Name: "selected", Path: source}, "windsurf", Options{
		CWD: selectedProject, CreateMissingAgentRoot: true,
	})
	if err != nil || !selected.Success || selected.Skipped {
		t.Fatalf("explicit target result = %#v, error %v", selected, err)
	}
	assertAgentLinkOrFallback(t, filepath.Join(selectedProject, ".windsurf", "skills", "selected"), selected.SymlinkFailed)

	claudeProject := filepath.Join(root, "claude-project")
	if err := os.MkdirAll(claudeProject, 0o755); err != nil {
		t.Fatal(err)
	}
	claude, err := InstallSkill(Skill{Name: "claude-default", Path: source}, "claude-code", Options{CWD: claudeProject})
	if err != nil || !claude.Success || claude.Skipped {
		t.Fatalf("Claude default-root result = %#v, error %v", claude, err)
	}
	assertAgentLinkOrFallback(t, filepath.Join(claudeProject, ".claude", "skills", "claude-default"), claude.SymlinkFailed)
}

func TestInstallSkillKeepsGlobalUniversalInstallInCanonicalDirectory(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeSkillSource(t, source, "global universal\n")

	result, err := InstallSkill(Skill{Name: "global-skill", Path: source}, "codex", Options{Global: true, CWD: project})
	if err != nil {
		t.Fatalf("global universal install: %v", err)
	}
	want := filepath.Join(home, ".agents", "skills", "global-skill")
	if !result.Success || result.Path != want || result.CanonicalPath != want {
		t.Fatalf("global universal result = %#v, want canonical path %q", result, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills")); !os.IsNotExist(err) {
		t.Fatalf("universal install created a duplicate agent directory: %v", err)
	}
}

func TestInstallSkillRejectsGlobalUnsupportedAgents(t *testing.T) {
	for _, id := range []agent.ID{"eve", "promptscript"} {
		t.Run(string(id), func(t *testing.T) {
			result, err := InstallSkill(Skill{Name: "skill", Path: t.TempDir()}, id, Options{Global: true})
			if err == nil || result.Success || result.Error == "" {
				t.Fatalf("unsupported global result = %#v, error %v", result, err)
			}
		})
	}
}

func TestInstallSkillPreservesSourceWhenDestinationOverlaps(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, "skills", "overlap")
	writeSkillSource(t, source, "do not delete\n")
	if err := os.WriteFile(filepath.Join(source, "support.txt"), []byte("still here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := InstallSkill(Skill{Name: "overlap", Path: source}, "openclaw", Options{CWD: project})
	if err != nil {
		t.Fatalf("overlapping install: %v", err)
	}
	if !result.Success || !result.Skipped || result.Path != source || result.CanonicalPath != "" {
		t.Fatalf("overlap result = %#v", result)
	}
	if contents, err := os.ReadFile(filepath.Join(source, "support.txt")); err != nil || string(contents) != "still here\n" {
		t.Fatalf("overlapping source contents = %q, error %v", contents, err)
	}
}

func TestInstallEveFiltersFrontmatterAndSupportsSubagents(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: eve-writer\ndescription: Eve skill\nlicense: MIT\ncompatibility:\n  editor: eve\nversion: 2\nmetadata:\n  owner: tools\nunsupported: remove-me\n---\n\n# Writer\n"
	if err := os.WriteFile(filepath.Join(source, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := InstallSkill(Skill{Name: "writer", Path: source}, "eve", Options{
		CWD: project, Mode: Copy, EveSubagent: "Research",
	})
	if err != nil {
		t.Fatalf("install Eve subagent: %v", err)
	}
	wantPath := filepath.Join(project, "agent", "subagents", "research", "skills", "writer")
	if !result.Success || result.Path != wantPath {
		t.Fatalf("Eve subagent result = %#v, want %q", result, wantPath)
	}
	installed, err := os.ReadFile(filepath.Join(wantPath, "skill.md"))
	if err != nil {
		t.Fatal(err)
	}
	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(string(installed), "---\n")), &frontmatter); err != nil {
		t.Fatalf("parse filtered Eve content: %v\n%s", err, installed)
	}
	if frontmatter["name"] != "eve-writer" || frontmatter["description"] != "Eve skill" || frontmatter["license"] != "MIT" || frontmatter["version"] != 2 {
		t.Errorf("Eve-compatible frontmatter = %#v", frontmatter)
	}
	if _, ok := frontmatter["unsupported"]; ok {
		t.Error("unsupported Eve frontmatter key was retained")
	}
	metadata, ok := frontmatter["metadata"].(map[string]any)
	if !ok || metadata["owner"] != "tools" {
		t.Errorf("Eve metadata = %#v", frontmatter["metadata"])
	}
	compatibility, ok := frontmatter["compatibility"].(map[string]any)
	if !ok || compatibility["editor"] != "eve" {
		t.Errorf("Eve compatibility = %#v", frontmatter["compatibility"])
	}
	if !strings.Contains(string(installed), "# Writer") {
		t.Errorf("Eve body was not retained: %s", installed)
	}

	rootResult, err := InstallSkill(Skill{Name: "eve-root", Path: source}, "eve", Options{CWD: project})
	if err != nil || !rootResult.Success {
		t.Fatalf("install Eve root skill = %#v, error %v", rootResult, err)
	}
	rootPath := filepath.Join(project, "agent", "skills", "eve-root")
	if rootResult.Path != rootPath || rootResult.CanonicalPath != rootPath {
		t.Fatalf("Eve root paths = %q / %q, want %q", rootResult.Path, rootResult.CanonicalPath, rootPath)
	}
}

func TestInstallEveRemovesOneLeadingLFOrCRLFAfterFiltering(t *testing.T) {
	project := t.TempDir()
	for _, test := range []struct {
		name    string
		content string
	}{
		{"filtered-crlf-frontmatter", "---\r\nunsupported: removed\r\n---\r\n\r\nbody\r\n"},
		{"no-frontmatter-crlf", "\r\nbody\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := InstallSkill(Skill{Name: test.name, Path: source}, "eve", Options{CWD: project, Mode: Copy})
			if err != nil || !result.Success {
				t.Fatalf("install Eve skill = %#v, error %v", result, err)
			}
			installed, err := os.ReadFile(filepath.Join(result.Path, "SKILL.md"))
			if err != nil || string(installed) != "body\r\n" {
				t.Fatalf("Eve content = %q, error %v, want body\\r\\n", installed, err)
			}
		})
	}
}

func TestInstallSkillUsesSameCanonicalDirectoryForSymlinkedAgentRoot(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	canonicalBase := filepath.Join(project, ".agents", "skills")
	if err := os.MkdirAll(canonicalBase, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonicalBase, filepath.Join(project, ".claude", "skills")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	writeSkillSource(t, source, "shared path\n")

	result, err := InstallSkill(Skill{Name: "shared", Path: source}, "claude-code", Options{CWD: project})
	if err != nil || !result.Success || result.SymlinkFailed {
		t.Fatalf("symlinked-root install = %#v, error %v", result, err)
	}
	installed := filepath.Join(canonicalBase, "shared", "SKILL.md")
	if contents, err := os.ReadFile(installed); err != nil || string(contents) != "shared path\n" {
		t.Fatalf("shared canonical skill = %q, error %v", contents, err)
	}
}

func writeSkillSource(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
