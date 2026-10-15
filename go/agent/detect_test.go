package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInstalledAgentsUsesProjectHomeAndEveDependency(t *testing.T) {
	clearDetectionEnvironment(t)
	home := t.TempDir()
	project := t.TempDir()
	configHome := filepath.Join(home, "config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	for _, path := range []string{
		filepath.Join(home, ".cline"),
		filepath.Join(home, ".cursor"),
		filepath.Join(project, ".replit"),
		filepath.Join(project, "agent"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "promptscript.yaml"), []byte("skills: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	packageJSON := map[string]any{"devDependencies": map[string]string{"eve": "^1.0.0"}}
	data, err := json.Marshal(packageJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "package.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := DetectInstalled(project)
	want := expectedDetectedAgents(map[ID]bool{"cline": true, "cursor": true, "eve": true, "replit": true, "promptscript": true})
	if !equalIDs(got, want) {
		t.Fatalf("detected agents = %v, want %v", got, want)
	}
}

func TestDetectInstalledAgentsHonorsEnvironmentAndLegacyLocations(t *testing.T) {
	clearDetectionEnvironment(t)
	home := t.TempDir()
	project := t.TempDir()
	configHome := filepath.Join(home, "config")
	customCodex := filepath.Join(t.TempDir(), "codex-home")
	customSarvam := filepath.Join(t.TempDir(), "sarvam-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("CODEX_HOME", customCodex)
	t.Setenv("SARVAM_HOME", customSarvam)

	for _, path := range []string{
		filepath.Join(home, ".clawdbot"),
		filepath.Join(home, ".kilocode"),
		filepath.Join(home, ".positai"),
		filepath.Join(configHome, "zed"),
		filepath.Join(configHome, "amp"),
		customCodex,
		customSarvam,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := DetectInstalled(project)
	want := expectedDetectedAgents(map[ID]bool{
		"amp": true, "openclaw": true, "codex": true, "kilo": true,
		"posit-assistant": true, "sarvam-code": true, "zed": true,
	})
	if !equalIDs(got, want) {
		t.Fatalf("detected agents = %v, want %v", got, want)
	}
}

func TestZedDetectionUsesPlatformConfigurationOverrides(t *testing.T) {
	clearDetectionEnvironment(t)
	home := t.TempDir()
	project := t.TempDir()
	appData := filepath.Join(t.TempDir(), "AppData")
	flatpakConfig := filepath.Join(t.TempDir(), "flatpak-config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", appData)
	t.Setenv("FLATPAK_XDG_CONFIG_HOME", flatpakConfig)
	for _, path := range []string{filepath.Join(appData, "Zed"), filepath.Join(flatpakConfig, "zed")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := DetectInstalled(project), []ID{"zed"}; !equalIDs(got, want) {
		t.Fatalf("platform-specific Zed detection = %v, want %v", got, want)
	}
}

func TestEveSubagentsReturnsOnlySortedDirectories(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, "agent", "subagents")
	for _, name := range []string{"writer", "research"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not a subagent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "writer"), filepath.Join(root, "linked")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if got, want := EveSubagents(project), []string{"research", "writer"}; !equalStrings(got, want) {
		t.Fatalf("Eve subagents = %v, want %v", got, want)
	}
	if got := EveSubagents(filepath.Join(project, "missing")); len(got) != 0 {
		t.Fatalf("missing Eve subagents = %v, want empty", got)
	}
}

func expectedDetectedAgents(expected map[ID]bool) []ID {
	if pathExists("/etc/codex") {
		expected["codex"] = true
	}
	if pathExists("/Applications/MiniMax Code.app") {
		expected["minimax-code"] = true
	}
	if pathExists("/Applications/ZCode.app") {
		expected["zcode"] = true
	}
	var result []ID
	for _, config := range Registry() {
		if expected[config.ID] {
			result = append(result, config.ID)
		}
	}
	return result
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func clearDetectionEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"XDG_CONFIG_HOME", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "VIBE_HOME", "HERMES_HOME", "AUTOHAND_HOME",
		"GROK_HOME", "SARVAM_HOME", "APPDATA", "FLATPAK_XDG_CONFIG_HOME",
	} {
		t.Setenv(key, "")
	}
}
