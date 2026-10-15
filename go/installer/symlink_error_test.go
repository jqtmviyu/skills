package installer

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyReturnsNonENOENTSymlinkErrors(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(source, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := os.Stat(loop); !errors.Is(err, syscall.ELOOP) {
		t.Skipf("platform does not report a symlink loop as ELOOP: %v", err)
	}

	result, err := InstallSkill(Skill{Name: "loop", Path: source}, "codex", Options{CWD: project, Mode: Copy})
	if err == nil || result.Success || result.Error == "" {
		t.Fatalf("non-ENOENT symlink result = %#v, error %v", result, err)
	}
}
