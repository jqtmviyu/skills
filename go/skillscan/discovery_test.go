package skillscan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalDiscoveryReadsSinglePluginManifestGrouping(t *testing.T) {
	repo := t.TempDir()
	writeSkill(t, repo, "skills/one", "single plugin skill", "A skill declared by plugin.json.")
	writeSkillFile(t, repo, ".claude-plugin/plugin.json", `{"name":"single-plugin","skills":["./skills/one","./../outside"]}`)

	scan, err := Open(context.Background(), repo, Options{})
	if err != nil {
		t.Fatalf("Open single-plugin repository: %v", err)
	}
	defer scan.Close()
	candidates := scan.Candidates()
	if got, want := candidatePaths(candidates), []string{"skills/one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("single-plugin candidates = %v, want %v", got, want)
	}
	if candidates[0].PluginName != "single-plugin" {
		t.Fatalf("single-plugin grouping = %q, want single-plugin", candidates[0].PluginName)
	}
}

func writeSkillFile(t *testing.T, root, relative, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create %s parent: %v", relative, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}
