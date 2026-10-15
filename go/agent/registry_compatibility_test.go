package agent

import (
	"path/filepath"
	"testing"
)

func TestRegistryProjectDirectoriesMatchNodeCompatibilityData(t *testing.T) {
	universal := []ID{
		"amp", "antigravity", "antigravity-cli", "cline", "codex", "cursor", "deepagents", "droid",
		"dexto", "firebender", "fx", "gemini-cli", "github-copilot", "kilo", "kimi-code-cli", "loaf", "opencode",
		"pi", "replit", "sarvam-code", "warp", "zed", "promptscript",
	}
	want := map[ID]string{}
	for _, id := range universal {
		want[id] = UniversalSkillsDir
	}
	for id, path := range map[ID]string{
		"aider-desk": ".aider-desk/skills", "astrbot": "data/skills", "autohand-code": ".autohand/skills",
		"augment": ".augment/skills", "bob": ".bob/skills", "claude-code": ".claude/skills",
		"codearts-agent": ".codeartsdoer/skills", "codebuddy": ".codebuddy/skills", "codemaker": ".codemaker/skills",
		"codestudio": ".codestudio/skills", "command-code": ".commandcode/skills", "continue": ".continue/skills",
		"cortex": ".cortex/skills", "crush": ".crush/skills", "devin": ".devin/skills", "forgecode": ".forge/skills",
		"goose": ".goose/skills", "grok": ".grok/skills", "hermes-agent": ".hermes/skills", "inference-sh": ".inferencesh/skills",
		"jazz": ".jazz/skills", "junie": ".junie/skills", "iflow-cli": ".iflow/skills", "kimchi": ".kimchi/skills",
		"kiro-cli": ".kiro/skills", "kode": ".kode/skills", "lingma": ".lingma/skills", "mcpjam": ".mcpjam/skills",
		"minimax-code": ".minimax/skills", "mistral-vibe": ".vibe/skills", "moxby": ".moxby/skills", "mux": ".mux/skills",
		"openhands": ".openhands/skills", "ona": ".ona/skills", "posit-assistant": ".posit/assistant/skills",
		"qoder": ".qoder/skills", "qoder-cn": ".qoder/skills", "qwen-code": ".qwen/skills", "reasonix": ".reasonix/skills",
		"rovodev": ".rovodev/skills", "roo": ".roo/skills", "tabnine-cli": ".tabnine/agent/skills",
		"terramind": ".terramind/skills", "tinycloud": ".tinycloud/skills", "trae": ".trae/skills",
		"trae-cn": ".trae/skills", "windsurf": ".windsurf/skills", "zcode": ".zcode/skills",
		"zencoder": ".zencoder/skills", "zenflow": ".zencoder/skills", "neovate": ".neovate/skills",
		"pochi": ".pochi/skills", "adal": ".adal/skills", "openclaw": "skills",
	} {
		want[id] = path
	}
	want["eve"] = "agent/skills"
	if len(want) != 78 {
		t.Fatalf("test compatibility table has %d IDs, want 78", len(want))
	}
	for _, config := range Registry() {
		if got, ok := want[config.ID]; !ok {
			t.Errorf("unexpected registry agent %q", config.ID)
		} else if config.SkillsDir != got {
			t.Errorf("%s project skills directory = %q, want %q", config.ID, config.SkillsDir, got)
		}
		delete(want, config.ID)
	}
	for id := range want {
		t.Errorf("registry is missing agent %q", id)
	}
}

func TestRegistryGlobalDirectoriesAndSupportMatchNodeCompatibilityData(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "VIBE_HOME", "HERMES_HOME", "AUTOHAND_HOME", "GROK_HOME"} {
		t.Setenv(key, "")
	}

	want := map[ID]string{}
	for id, suffix := range map[ID]string{
		"aider-desk": ".aider-desk/skills", "antigravity": ".gemini/antigravity/skills", "antigravity-cli": ".gemini/antigravity-cli/skills",
		"astrbot": ".astrbot/data/skills", "autohand-code": ".autohand/skills", "augment": ".augment/skills", "bob": ".bob/skills",
		"claude-code": ".claude/skills", "openclaw": ".openclaw/skills", "cline": ".agents/skills", "codearts-agent": ".codeartsdoer/skills",
		"codebuddy": ".codebuddy/skills", "codemaker": ".codemaker/skills", "codestudio": ".codestudio/skills", "codex": ".codex/skills",
		"command-code": ".commandcode/skills", "continue": ".continue/skills", "cortex": ".snowflake/cortex/skills", "cursor": ".cursor/skills",
		"deepagents": ".deepagents/agent/skills", "dexto": ".agents/skills", "droid": ".factory/skills", "firebender": ".firebender/skills",
		"forgecode": ".forge/skills", "fx": ".agents/skills", "gemini-cli": ".gemini/skills", "github-copilot": ".copilot/skills",
		"grok": ".grok/skills", "hermes-agent": ".hermes/skills", "inference-sh": ".inferencesh/skills", "jazz": ".jazz/skills",
		"junie": ".junie/skills", "iflow-cli": ".iflow/skills", "kilo": ".kilo/skills", "kimchi": ".config/kimchi/harness/skills",
		"kimi-code-cli": ".agents/skills", "kiro-cli": ".kiro/skills", "kode": ".kode/skills", "lingma": ".lingma/skills",
		"loaf": ".agents/skills", "mcpjam": ".mcpjam/skills", "minimax-code": ".minimax/skills", "mistral-vibe": ".vibe/skills",
		"moxby": ".moxby/skills", "mux": ".mux/skills", "openhands": ".openhands/skills", "ona": ".ona/skills",
		"pi": ".agents/skills", "posit-assistant": ".posit/assistant/skills", "qoder": ".qoder/skills", "qoder-cn": ".qoder-cn/skills",
		"qwen-code": ".qwen/skills", "reasonix": ".reasonix/skills", "rovodev": ".rovodev/skills", "roo": ".roo/skills",
		"sarvam-code": ".agents/skills", "tabnine-cli": ".tabnine/agent/skills", "terramind": ".terramind/skills",
		"tinycloud": ".tinycloud/skills", "trae": ".trae/skills", "trae-cn": ".trae-cn/skills", "warp": ".agents/skills",
		"windsurf": ".codeium/windsurf/skills", "zed": ".agents/skills", "zcode": ".zcode/skills", "zencoder": ".zencoder/skills",
		"zenflow": ".zencoder/skills", "neovate": ".neovate/skills", "pochi": ".pochi/skills", "adal": ".adal/skills",
	} {
		want[id] = filepath.Join(home, filepath.FromSlash(suffix))
	}
	for id, suffix := range map[ID]string{
		"amp": "agents/skills", "crush": "crush/skills", "devin": "devin/skills", "goose": "goose/skills",
		"opencode": "opencode/skills", "replit": "agents/skills",
	} {
		want[id] = filepath.Join(configHome, filepath.FromSlash(suffix))
	}
	want["eve"] = ""
	want["promptscript"] = ""
	if len(want) != 78 {
		t.Fatalf("test global compatibility table has %d IDs, want 78", len(want))
	}
	for _, config := range Registry() {
		if got, ok := want[config.ID]; !ok {
			t.Errorf("unexpected registry agent %q", config.ID)
		} else if config.GlobalSkillsDir != got {
			t.Errorf("%s global skills directory = %q, want %q", config.ID, config.GlobalSkillsDir, got)
		}
		if config.SupportsGlobal != (config.GlobalSkillsDir != "") {
			t.Errorf("%s global support = %v, but global path is %q", config.ID, config.SupportsGlobal, config.GlobalSkillsDir)
		}
		delete(want, config.ID)
	}
	for id := range want {
		t.Errorf("registry is missing global path for %q", id)
	}
}

func TestRegistryUniversalVisibilityAndGlobalSupportFlags(t *testing.T) {
	if got, want := UniversalAgents(), []ID{
		"amp", "antigravity", "antigravity-cli", "cline", "codex", "cursor", "deepagents", "dexto", "droid", "firebender", "fx", "gemini-cli",
		"github-copilot", "kilo", "kimi-code-cli", "loaf", "opencode", "pi", "sarvam-code", "warp", "zed", "promptscript",
	}; !equalIDs(got, want) {
		t.Errorf("universal agents = %v, want %v", got, want)
	}
	if got, want := VisibleUniversalAgents(), []ID{
		"amp", "cline", "codex", "cursor", "droid", "fx", "gemini-cli", "github-copilot", "kilo", "kimi-code-cli", "opencode", "pi", "warp", "zed",
	}; !equalIDs(got, want) {
		t.Errorf("visible universal agents = %v, want %v", got, want)
	}
	unsupported := []ID{}
	for _, config := range Registry() {
		if !config.SupportsGlobal {
			unsupported = append(unsupported, config.ID)
		}
	}
	if !equalIDs(unsupported, []ID{"eve", "promptscript"}) {
		t.Errorf("agents without global install support = %v", unsupported)
	}
	if !IsUniversal("universal") || IsUniversal("claude-code") {
		t.Fatal("universal classification did not follow project skills directory")
	}
}

func TestAgentDirectoryEnvironmentOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, override := range []struct {
		id  ID
		env string
	}{
		{"codex", "CODEX_HOME"}, {"claude-code", "CLAUDE_CONFIG_DIR"}, {"mistral-vibe", "VIBE_HOME"},
		{"hermes-agent", "HERMES_HOME"}, {"autohand-code", "AUTOHAND_HOME"}, {"grok", "GROK_HOME"},
	} {
		t.Run(string(override.id), func(t *testing.T) {
			custom := filepath.Join(t.TempDir(), "custom-agent-home")
			t.Setenv(override.env, "  "+custom+"  ")
			config, ok := ConfigFor(override.id)
			if !ok {
				t.Fatalf("missing config for %s", override.id)
			}
			if got, want := config.GlobalSkillsDir, filepath.Join(custom, "skills"); got != want {
				t.Errorf("global skills directory = %q, want %q", got, want)
			}
		})
	}
}

func equalIDs(got, want []ID) bool {
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
