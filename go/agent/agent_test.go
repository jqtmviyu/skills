package agent

import (
	"sort"
	"testing"
)

func TestRegistryContainsEverySupportedAgent(t *testing.T) {
	want := []ID{
		"aider-desk", "amp", "antigravity", "antigravity-cli", "astrbot", "autohand-code",
		"augment", "bob", "claude-code", "openclaw", "cline", "codearts-agent", "codebuddy",
		"codemaker", "codestudio", "codex", "command-code", "continue", "cortex", "crush",
		"cursor", "deepagents", "devin", "dexto", "droid", "eve", "firebender", "forgecode",
		"fx", "gemini-cli", "github-copilot", "goose", "grok", "hermes-agent", "inference-sh",
		"jazz", "junie", "iflow-cli", "kilo", "kimchi", "kimi-code-cli", "kiro-cli", "kode",
		"lingma", "loaf", "mcpjam", "minimax-code", "mistral-vibe", "moxby", "mux", "opencode",
		"openhands", "ona", "pi", "posit-assistant", "qoder", "qoder-cn", "qwen-code", "replit",
		"reasonix", "rovodev", "roo", "sarvam-code", "tabnine-cli", "terramind", "tinycloud",
		"trae", "trae-cn", "warp", "windsurf", "zed", "zcode", "zencoder", "zenflow", "neovate",
		"pochi", "promptscript", "adal",
	}
	got := make([]ID, 0, len(Registry()))
	for _, config := range Registry() {
		got = append(got, config.ID)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(got) != len(want) {
		t.Fatalf("registry has %d IDs, want all %d supported agents", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("registry ID %q, want %q", got[index], want[index])
		}
	}
}
