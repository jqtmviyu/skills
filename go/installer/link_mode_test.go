package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLinkModeLinksTheCanonicalSkillToItsSource(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	writeSkillSource(t, source, "linked from source\n")

	result, err := InstallSkill(Skill{Name: "linked", Path: source}, "codex", Options{CWD: project, Mode: Link})
	if err != nil {
		t.Fatalf("link-mode install: %v", err)
	}
	canonical := filepath.Join(project, ".agents", "skills", "linked")
	if !result.Success || result.Mode != Link || result.Path != canonical {
		t.Fatalf("link-mode result = %#v", result)
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 && (runtime.GOOS != "windows" || !info.IsDir()) {
		t.Fatalf("canonical path mode = %v, want symlink (or Windows copy fallback)", info.Mode())
	}
	if contents, err := os.ReadFile(filepath.Join(canonical, "SKILL.md")); err != nil || string(contents) != "linked from source\n" {
		t.Fatalf("linked source contents = %q, error %v", contents, err)
	}
}
