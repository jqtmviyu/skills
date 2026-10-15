package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// ID identifies an agent supported by the current Skills CLI.
type ID string

const (
	UniversalID        ID = "universal"
	UniversalSkillsDir    = ".agents/skills"
)

// Config contains the resolved install directories and compatibility policies
// for one agent. GlobalSkillsDir is empty when global installation is unsupported.
type Config struct {
	ID                              ID
	DisplayName                     string
	SkillsDir                       string
	GlobalSkillsDir                 string
	SupportsGlobal                  bool
	ShowInUniversalList             bool
	ShowInUniversalPrompt           bool
	CreateProjectSkillsDirByDefault bool
}

type definition struct {
	id                              ID
	displayName                     string
	projectDir                      string
	globalDir                       string
	showInUniversalList             bool
	showInUniversalPrompt           bool
	createProjectSkillsDirByDefault bool
}

// Definitions follows the Node registry order. The virtual "universal" target
// is resolved by ConfigFor but is not counted as one of the 78 installed agents.
var definitions = []definition{
	{"aider-desk", "AiderDesk", ".aider-desk/skills", "home:.aider-desk/skills", true, true, false},
	{"amp", "Amp", UniversalSkillsDir, "config:agents/skills", true, true, false},
	{"antigravity", "Antigravity", UniversalSkillsDir, "home:.gemini/antigravity/skills", true, false, false},
	{"antigravity-cli", "Antigravity CLI", UniversalSkillsDir, "home:.gemini/antigravity-cli/skills", true, false, false},
	{"astrbot", "AstrBot", "data/skills", "home:.astrbot/data/skills", true, true, false},
	{"autohand-code", "Autohand Code CLI", ".autohand/skills", "env:AUTOHAND_HOME:.autohand/skills", true, true, false},
	{"augment", "Augment", ".augment/skills", "home:.augment/skills", true, true, false},
	{"bob", "IBM Bob", ".bob/skills", "home:.bob/skills", true, true, false},
	{"claude-code", "Claude Code", ".claude/skills", "env:CLAUDE_CONFIG_DIR:.claude/skills", true, true, true},
	{"openclaw", "OpenClaw", "skills", "special:openclaw", true, true, false},
	{"cline", "Cline", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"codearts-agent", "CodeArts Agent", ".codeartsdoer/skills", "home:.codeartsdoer/skills", true, true, false},
	{"codebuddy", "CodeBuddy", ".codebuddy/skills", "home:.codebuddy/skills", true, true, false},
	{"codemaker", "Codemaker", ".codemaker/skills", "home:.codemaker/skills", true, true, false},
	{"codestudio", "Code Studio", ".codestudio/skills", "home:.codestudio/skills", true, true, false},
	{"codex", "Codex", UniversalSkillsDir, "env:CODEX_HOME:.codex/skills", true, true, false},
	{"command-code", "Command Code", ".commandcode/skills", "home:.commandcode/skills", true, true, false},
	{"continue", "Continue", ".continue/skills", "home:.continue/skills", true, true, false},
	{"cortex", "Cortex Code", ".cortex/skills", "home:.snowflake/cortex/skills", true, true, false},
	{"crush", "Crush", ".crush/skills", "config:crush/skills", true, true, false},
	{"cursor", "Cursor", UniversalSkillsDir, "home:.cursor/skills", true, true, false},
	{"deepagents", "Deep Agents", UniversalSkillsDir, "home:.deepagents/agent/skills", true, false, false},
	{"devin", "Devin for Terminal", ".devin/skills", "config:devin/skills", true, true, false},
	{"dexto", "Dexto", UniversalSkillsDir, "home:.agents/skills", true, false, false},
	{"droid", "Droid", UniversalSkillsDir, "home:.factory/skills", true, true, false},
	{"eve", "Eve", "agent/skills", "", true, true, false},
	{"firebender", "Firebender", UniversalSkillsDir, "home:.firebender/skills", true, false, false},
	{"forgecode", "ForgeCode", ".forge/skills", "home:.forge/skills", true, true, false},
	{"fx", "fx", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"gemini-cli", "Gemini CLI", UniversalSkillsDir, "home:.gemini/skills", true, true, false},
	{"github-copilot", "GitHub Copilot", UniversalSkillsDir, "home:.copilot/skills", true, true, false},
	{"goose", "Goose", ".goose/skills", "config:goose/skills", true, true, false},
	{"grok", "Grok Build", ".grok/skills", "env:GROK_HOME:.grok/skills", true, true, false},
	{"hermes-agent", "Hermes Agent", ".hermes/skills", "env:HERMES_HOME:.hermes/skills", true, true, false},
	{"inference-sh", "inference.sh", ".inferencesh/skills", "home:.inferencesh/skills", true, true, false},
	{"jazz", "Jazz", ".jazz/skills", "home:.jazz/skills", true, true, false},
	{"junie", "Junie", ".junie/skills", "home:.junie/skills", true, true, false},
	{"iflow-cli", "iFlow CLI", ".iflow/skills", "home:.iflow/skills", true, true, false},
	{"kilo", "Kilo Code", UniversalSkillsDir, "home:.kilo/skills", true, true, false},
	{"kimchi", "Kimchi", ".kimchi/skills", "home:.config/kimchi/harness/skills", true, true, false},
	{"kimi-code-cli", "Kimi Code CLI", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"kiro-cli", "Kiro CLI", ".kiro/skills", "home:.kiro/skills", true, true, false},
	{"kode", "Kode", ".kode/skills", "home:.kode/skills", true, true, false},
	{"lingma", "Lingma", ".lingma/skills", "home:.lingma/skills", true, true, false},
	{"loaf", "Loaf", UniversalSkillsDir, "home:.agents/skills", true, false, false},
	{"mcpjam", "MCPJam", ".mcpjam/skills", "home:.mcpjam/skills", true, true, false},
	{"minimax-code", "MiniMax Code", ".minimax/skills", "home:.minimax/skills", true, true, false},
	{"mistral-vibe", "Mistral Vibe", ".vibe/skills", "env:VIBE_HOME:.vibe/skills", true, true, false},
	{"moxby", "Moxby", ".moxby/skills", "home:.moxby/skills", true, true, false},
	{"mux", "Mux", ".mux/skills", "home:.mux/skills", true, true, false},
	{"opencode", "OpenCode", UniversalSkillsDir, "config:opencode/skills", true, true, false},
	{"openhands", "OpenHands", ".openhands/skills", "home:.openhands/skills", true, true, false},
	{"ona", "Ona", ".ona/skills", "home:.ona/skills", true, true, false},
	{"pi", "Pi", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"posit-assistant", "Posit Assistant", ".posit/assistant/skills", "home:.posit/assistant/skills", true, true, false},
	{"qoder", "Qoder", ".qoder/skills", "home:.qoder/skills", true, true, false},
	{"qoder-cn", "Qoder CN", ".qoder/skills", "home:.qoder-cn/skills", true, true, false},
	{"qwen-code", "Qwen Code", ".qwen/skills", "home:.qwen/skills", true, true, false},
	{"replit", "Replit", UniversalSkillsDir, "config:agents/skills", false, true, false},
	{"reasonix", "Reasonix", ".reasonix/skills", "home:.reasonix/skills", true, true, false},
	{"rovodev", "Rovo Dev", ".rovodev/skills", "home:.rovodev/skills", true, true, false},
	{"roo", "Roo Code", ".roo/skills", "home:.roo/skills", true, true, false},
	{"sarvam-code", "Sarvam Code", UniversalSkillsDir, "home:.agents/skills", true, false, false},
	{"tabnine-cli", "Tabnine CLI", ".tabnine/agent/skills", "home:.tabnine/agent/skills", true, true, false},
	{"terramind", "Terramind", ".terramind/skills", "home:.terramind/skills", true, true, false},
	{"tinycloud", "Tinycloud", ".tinycloud/skills", "home:.tinycloud/skills", true, true, false},
	{"trae", "Trae", ".trae/skills", "home:.trae/skills", true, true, false},
	{"trae-cn", "Trae CN", ".trae/skills", "home:.trae-cn/skills", true, true, false},
	{"warp", "Warp", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"windsurf", "Windsurf", ".windsurf/skills", "home:.codeium/windsurf/skills", true, true, false},
	{"zed", "Zed", UniversalSkillsDir, "home:.agents/skills", true, true, false},
	{"zcode", "ZCode", ".zcode/skills", "home:.zcode/skills", true, true, false},
	{"zencoder", "Zencoder", ".zencoder/skills", "home:.zencoder/skills", true, true, false},
	{"zenflow", "Zenflow", ".zencoder/skills", "home:.zencoder/skills", true, true, false},
	{"neovate", "Neovate", ".neovate/skills", "home:.neovate/skills", true, true, false},
	{"pochi", "Pochi", ".pochi/skills", "home:.pochi/skills", true, true, false},
	{"promptscript", "PromptScript", UniversalSkillsDir, "", true, false, false},
	{"adal", "AdaL", ".adal/skills", "home:.adal/skills", true, true, false},
}

// Registry returns the 78 concrete agent configurations in Node registry order.
func Registry() []Config {
	home := homeDirectory()
	configHome := xdgConfigHome(home)
	configs := make([]Config, 0, len(definitions))
	for _, definition := range definitions {
		configs = append(configs, configForDefinition(definition, home, configHome))
	}
	return configs
}

// ConfigFor returns a supported agent configuration. "universal" is a virtual
// install target that resolves to the shared .agents/skills directory.
func ConfigFor(id ID) (Config, bool) {
	if id == UniversalID {
		return Config{
			ID: id, DisplayName: "Universal", SkillsDir: UniversalSkillsDir,
			GlobalSkillsDir:     filepath.Join(xdgConfigHome(homeDirectory()), "agents", "skills"),
			SupportsGlobal:      true,
			ShowInUniversalList: false, ShowInUniversalPrompt: true,
		}, true
	}
	for _, definition := range definitions {
		if definition.id == id {
			return configForDefinition(definition, homeDirectory(), xdgConfigHome(homeDirectory())), true
		}
	}
	return Config{}, false
}

func configForDefinition(definition definition, home, configHome string) Config {
	return Config{
		ID:                              definition.id,
		DisplayName:                     definition.displayName,
		SkillsDir:                       definition.projectDir,
		GlobalSkillsDir:                 resolveGlobalDir(definition.globalDir, home, configHome),
		SupportsGlobal:                  definition.globalDir != "",
		ShowInUniversalList:             definition.showInUniversalList,
		ShowInUniversalPrompt:           definition.showInUniversalPrompt,
		CreateProjectSkillsDirByDefault: definition.createProjectSkillsDirByDefault,
	}
}

func resolveGlobalDir(spec, home, configHome string) string {
	if spec == "" {
		return ""
	}
	if spec == "special:openclaw" {
		for _, configRoot := range []string{".openclaw", ".clawdbot", ".moltbot"} {
			if pathExists(filepath.Join(home, configRoot)) {
				return filepath.Join(home, configRoot, "skills")
			}
		}
		return filepath.Join(home, ".openclaw", "skills")
	}
	if strings.HasPrefix(spec, "home:") {
		return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(spec, "home:")))
	}
	if strings.HasPrefix(spec, "config:") {
		return filepath.Join(configHome, filepath.FromSlash(strings.TrimPrefix(spec, "config:")))
	}
	if strings.HasPrefix(spec, "env:") {
		parts := strings.SplitN(strings.TrimPrefix(spec, "env:"), ":", 2)
		if len(parts) == 2 {
			base := strings.TrimSpace(os.Getenv(parts[0]))
			if base == "" {
				base = filepath.Join(home, filepath.FromSlash(parts[1]))
				return base
			}
			return filepath.Join(base, filepath.Base(filepath.FromSlash(parts[1])))
		}
	}
	return ""
}

func homeDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func xdgConfigHome(home string) string {
	if configured := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configured != "" && filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(home, ".config")
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsUniversal follows the Node compatibility rule: universal status comes from
// an agent's project SkillsDir, not its global directory or ID.
func IsUniversal(id ID) bool {
	config, ok := ConfigFor(id)
	return ok && config.SkillsDir == UniversalSkillsDir
}

// UniversalAgents returns universal agents that are shown in the agent list.
func UniversalAgents() []ID {
	var result []ID
	for _, config := range Registry() {
		if config.SkillsDir == UniversalSkillsDir && config.ShowInUniversalList {
			result = append(result, config.ID)
		}
	}
	return result
}

// VisibleUniversalAgents excludes universal agents hidden from the prompt.
func VisibleUniversalAgents() []ID {
	var result []ID
	for _, config := range Registry() {
		if config.SkillsDir == UniversalSkillsDir && config.ShowInUniversalList && config.ShowInUniversalPrompt {
			result = append(result, config.ID)
		}
	}
	return result
}

// NonUniversalAgents returns agents with agent-specific project directories.
func NonUniversalAgents() []ID {
	var result []ID
	for _, config := range Registry() {
		if config.SkillsDir != UniversalSkillsDir {
			result = append(result, config.ID)
		}
	}
	return result
}
