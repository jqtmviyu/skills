package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyModePreservesFilesExclusionsModesAndDereferencesSourceLinks(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	source := filepath.Join(root, "source")
	external := filepath.Join(root, "external")
	for _, path := range []string{project, source, filepath.Join(source, "scripts"), filepath.Join(source, ".git"), filepath.Join(source, "__pycache__"), filepath.Join(source, "__pypackages__"), external} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, contents string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(source, "SKILL.md"), "skill\n", 0o644)
	write(filepath.Join(source, ".hidden"), "keep\n", 0o644)
	write(filepath.Join(source, "metadata.json"), "exclude\n", 0o644)
	write(filepath.Join(source, ".git", "config"), "exclude\n", 0o644)
	write(filepath.Join(source, "__pycache__", "cache.pyc"), "exclude\n", 0o644)
	write(filepath.Join(source, "__pypackages__", "pkg"), "exclude\n", 0o644)
	script := filepath.Join(source, "scripts", "run.sh")
	write(script, "#!/bin/sh\n", 0o755)
	write(filepath.Join(external, "linked-file.txt"), "linked file\n", 0o600)
	if err := os.Mkdir(filepath.Join(external, "linked-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(external, "linked-dir", "nested.txt"), "nested link\n", 0o644)
	if err := os.Symlink(filepath.Join(root, "nested-missing-target"), filepath.Join(external, "linked-dir", "broken-link")); err != nil {
		t.Skipf("nested symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(external, "linked-file.txt"), filepath.Join(source, "linked-file.txt")); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(external, "linked-dir"), filepath.Join(source, "linked-dir")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(source, "broken-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var warnings bytes.Buffer

	result, err := InstallSkill(Skill{Name: "copy-tree", Path: source, PluginName: "copy-group"}, "codex", Options{CWD: project, Mode: Copy, Warnings: &warnings})
	if err != nil {
		t.Fatalf("copy install: %v", err)
	}
	destination := filepath.Join(project, ".agents", "skills", "copy-tree")
	if !result.Success || result.Mode != Copy || result.Path != destination || result.CanonicalPath != "" || result.PluginName != "copy-group" {
		t.Fatalf("copy result = %#v", result)
	}
	for name, want := range map[string]string{
		"SKILL.md": "skill\n", ".hidden": "keep\n", "linked-file.txt": "linked file\n", "linked-dir/nested.txt": "nested link\n",
	} {
		contents, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil || string(contents) != want {
			t.Errorf("copied %s = %q, error %v", name, contents, err)
		}
	}
	for _, name := range []string{"metadata.json", ".git", "__pycache__", "__pypackages__"} {
		if _, err := os.Lstat(filepath.Join(destination, name)); !os.IsNotExist(err) {
			t.Errorf("excluded entry %s exists or returned unexpected error: %v", name, err)
		}
	}
	sourceMode, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	installedMode, err := os.Stat(filepath.Join(destination, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := installedMode.Mode().Perm(), sourceMode.Mode().Perm(); got != want {
		t.Errorf("copied script permissions = %o, want %o", got, want)
	}
	wantWarnings := "Skipping broken symlink: " + filepath.Join(source, "broken-link") + "\n" +
		"Skipping broken symlink: " + filepath.Join(source, "linked-dir", "broken-link") + "\n"
	if got := warnings.String(); got != wantWarnings {
		t.Errorf("broken-symlink warning = %q, want %q", got, wantWarnings)
	}
	for _, name := range []string{"linked-file.txt", "linked-dir"} {
		info, err := os.Lstat(filepath.Join(destination, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("source symlink %s was copied as a symlink", name)
		}
	}
}
