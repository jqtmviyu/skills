package installer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkModeCopiesSourceWhenItsCanonicalSymlinkFails(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	writeSkillSource(t, source, "copied after link failure\n")

	result, err := InstallSkill(Skill{Name: "link-fallback", Path: source}, "codex", Options{
		CWD: project, Mode: Link,
		CreateSymlink: func(string, string) error { return errors.New("symlinks unavailable") },
	})
	if err != nil {
		t.Fatalf("link fallback install: %v", err)
	}
	canonical := filepath.Join(project, ".agents", "skills", "link-fallback")
	if !result.Success || result.Path != canonical || result.SymlinkFailed {
		t.Fatalf("link fallback result = %#v", result)
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("link fallback canonical path mode = %v", info.Mode())
	}
}
