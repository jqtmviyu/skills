package skillscan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalSourceDiscoversWithRootPrecedenceAndFullDepth(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "", "repository", "The repository root skill.")
	writeSkill(t, repo, "skills/alpha", "alpha", "A nested skill.")

	scanStorage := t.TempDir()
	defaultScan, err := Open(context.Background(), repo, Options{TempDir: scanStorage})
	if err != nil {
		t.Fatalf("Open local source: %v", err)
	}
	defer defaultScan.Close()
	if defaultScan.Source().Kind != LocalSource {
		t.Fatalf("source kind = %q, want local", defaultScan.Source().Kind)
	}
	if entries, err := os.ReadDir(scanStorage); err != nil || len(entries) != 0 {
		t.Fatalf("local source created scan artifacts: entries=%v err=%v", entries, err)
	}
	if got, want := candidatePaths(defaultScan.Candidates()), []string{"."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("default candidates = %v, want %v", got, want)
	}

	fullScan, err := Open(context.Background(), repo, Options{FullDepth: true, TempDir: scanStorage})
	if err != nil {
		t.Fatalf("Open local source with full depth: %v", err)
	}
	defer fullScan.Close()
	if entries, err := os.ReadDir(scanStorage); err != nil || len(entries) != 0 {
		t.Fatalf("full-depth local scan created scan artifacts: entries=%v err=%v", entries, err)
	}
	if got, want := candidatePaths(fullScan.Candidates()), []string{".", "skills/alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("full-depth candidates = %v, want %v", got, want)
	}

	selected, err := fullScan.Materialize([]string{"skills/alpha"})
	if err != nil {
		t.Fatalf("materialize local skill: %v", err)
	}
	if len(selected) != 1 || selected[0].Directory != filepath.Join(repo, "skills/alpha") {
		t.Fatalf("materialized local skill = %#v", selected)
	}
	if _, err := os.Stat(filepath.Join(repo, "skills/alpha/SKILL.md")); err != nil {
		t.Fatalf("local source was changed or removed: %v", err)
	}
}

func TestLocalDiscoveryExcludesProjectSkillsAlreadyRecordedInLock(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, ".claude/skills/installed_skill", "Renamed Installed Skill", "Already installed.")
	writeSkill(t, repo, ".claude/skills/available", "Available", "Not in the lock.")
	lock := `{"version":1,"skills":{"installed-skill":{"source":"source","sourceType":"github"}}}`
	writeSkillFile(t, repo, "skills-lock.json", lock)

	scan, err := Open(context.Background(), repo, Options{})
	if err != nil {
		t.Fatalf("Open local source with project lock: %v", err)
	}
	defer scan.Close()
	if got, want := candidatePaths(scan.Candidates()), []string{".claude/skills/available"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates with project lock = %v, want %v", got, want)
	}
}

func writeSkill(t *testing.T, root, dir, name, description string) {
	t.Helper()
	path := filepath.Join(root, dir, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func candidatePaths(candidates []Candidate) []string {
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		paths = append(paths, candidate.Path)
	}
	return paths
}
