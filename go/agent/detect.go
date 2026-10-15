package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const EveSubagentsDir = "agent/subagents"

var detectionPaths = map[ID][]string{
	"aider-desk":      {"home:.aider-desk"},
	"amp":             {"config:amp"},
	"antigravity":     {"home:.gemini/antigravity"},
	"antigravity-cli": {"home:.gemini/antigravity-cli"},
	"astrbot":         {"cwd:data/skills", "home:.astrbot"},
	"autohand-code":   {"envroot:AUTOHAND_HOME:.autohand"},
	"augment":         {"home:.augment"},
	"bob":             {"home:.bob"},
	"claude-code":     {"envroot:CLAUDE_CONFIG_DIR:.claude"},
	"openclaw":        {"home:.openclaw", "home:.clawdbot", "home:.moltbot"},
	"cline":           {"home:.cline"},
	"codearts-agent":  {"home:.codeartsdoer"},
	"codebuddy":       {"cwd:.codebuddy", "home:.codebuddy"},
	"codemaker":       {"home:.codemaker"},
	"codestudio":      {"home:.codestudio"},
	"codex":           {"envroot:CODEX_HOME:.codex", "absolute:/etc/codex"},
	"command-code":    {"home:.commandcode"},
	"continue":        {"cwd:.continue", "home:.continue"},
	"cortex":          {"home:.snowflake/cortex"},
	"crush":           {"config:crush"},
	"cursor":          {"home:.cursor"},
	"deepagents":      {"home:.deepagents"},
	"devin":           {"config:devin"},
	"dexto":           {"home:.dexto"},
	"droid":           {"home:.factory"},
	"firebender":      {"home:.firebender"},
	"forgecode":       {"home:.forge"},
	"fx":              {"home:.fx"},
	"gemini-cli":      {"home:.gemini"},
	"github-copilot":  {"home:.copilot"},
	"goose":           {"config:goose"},
	"grok":            {"envroot:GROK_HOME:.grok"},
	"hermes-agent":    {"envroot:HERMES_HOME:.hermes"},
	"inference-sh":    {"home:.inferencesh"},
	"jazz":            {"home:.jazz", "cwd:.jazz"},
	"junie":           {"home:.junie"},
	"iflow-cli":       {"home:.iflow"},
	"kilo":            {"home:.kilo", "home:.kilocode"},
	"kimchi":          {"home:.config/kimchi"},
	"kimi-code-cli":   {"home:.kimi-code", "home:.kimi"},
	"kiro-cli":        {"home:.kiro"},
	"kode":            {"home:.kode"},
	"lingma":          {"home:.lingma"},
	"loaf":            {"home:.loaf"},
	"mcpjam":          {"home:.mcpjam"},
	"minimax-code":    {"home:.minimax", "absolute:/Applications/MiniMax Code.app"},
	"mistral-vibe":    {"envroot:VIBE_HOME:.vibe"},
	"moxby":           {"home:.moxby"},
	"mux":             {"home:.mux"},
	"opencode":        {"config:opencode"},
	"openhands":       {"home:.openhands"},
	"ona":             {"home:.ona"},
	"pi":              {"home:.pi/agent"},
	"posit-assistant": {"home:.posit/assistant", "home:.positai"},
	"qoder":           {"home:.qoder"},
	"qoder-cn":        {"home:.qoder-cn"},
	"qwen-code":       {"home:.qwen"},
	"replit":          {"cwd:.replit"},
	"reasonix":        {"home:.reasonix"},
	"rovodev":         {"home:.rovodev"},
	"roo":             {"home:.roo"},
	"sarvam-code":     {"envroot:SARVAM_HOME:.sarvam"},
	"tabnine-cli":     {"home:.tabnine"},
	"terramind":       {"home:.terramind"},
	"tinycloud":       {"home:.tinycloud"},
	"trae":            {"home:.trae"},
	"trae-cn":         {"home:.trae-cn"},
	"warp":            {"home:.warp"},
	"windsurf":        {"home:.codeium/windsurf"},
	"zed":             {"config:zed", "envpath:APPDATA:Zed", "envpath:FLATPAK_XDG_CONFIG_HOME:zed"},
	"zcode":           {"home:.zcode", "absolute:/Applications/ZCode.app"},
	"zencoder":        {"home:.zencoder"},
	"zenflow":         {"home:.zencoder"},
	"neovate":         {"home:.neovate"},
	"pochi":           {"home:.pochi"},
	"promptscript":    {"cwd:.promptscript", "cwd:promptscript.yaml"},
	"adal":            {"home:.adal"},
}

// DetectInstalled returns detected agents in registry order. cwd is used for
// project-sensitive checks; an empty value means the current working directory.
func DetectInstalled(cwd string) []ID {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	home := homeDirectory()
	configHome := xdgConfigHome(home)
	var detected []ID
	for _, config := range Registry() {
		installed := false
		if config.ID == "eve" {
			installed = pathExists(filepath.Join(cwd, "agent")) && packageHasEveDependency(filepath.Join(cwd, "package.json"))
		} else {
			for _, spec := range detectionPaths[config.ID] {
				if pathExists(resolveDetectionPath(spec, home, configHome, cwd)) {
					installed = true
					break
				}
			}
		}
		if installed {
			detected = append(detected, config.ID)
		}
	}
	return detected
}

// EveSubagents lists real subagent directories in alphabetical order. Files,
// symlinked directories, missing roots, and unreadable roots are ignored.
func EveSubagents(cwd string) []string {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil
		}
	}
	entries, err := os.ReadDir(filepath.Join(cwd, filepath.FromSlash(EveSubagentsDir)))
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result
}

func resolveDetectionPath(spec, home, configHome, cwd string) string {
	if strings.HasPrefix(spec, "home:") {
		return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(spec, "home:")))
	}
	if strings.HasPrefix(spec, "config:") {
		return filepath.Join(configHome, filepath.FromSlash(strings.TrimPrefix(spec, "config:")))
	}
	if strings.HasPrefix(spec, "cwd:") {
		return filepath.Join(cwd, filepath.FromSlash(strings.TrimPrefix(spec, "cwd:")))
	}
	if strings.HasPrefix(spec, "absolute:") {
		return filepath.FromSlash(strings.TrimPrefix(spec, "absolute:"))
	}
	if strings.HasPrefix(spec, "envroot:") {
		parts := strings.SplitN(strings.TrimPrefix(spec, "envroot:"), ":", 2)
		if len(parts) == 2 {
			if configured := strings.TrimSpace(os.Getenv(parts[0])); configured != "" {
				return configured
			}
			return filepath.Join(home, filepath.FromSlash(parts[1]))
		}
	}
	if strings.HasPrefix(spec, "envpath:") {
		parts := strings.SplitN(strings.TrimPrefix(spec, "envpath:"), ":", 2)
		if len(parts) == 2 {
			if configured := strings.TrimSpace(os.Getenv(parts[0])); configured != "" {
				return filepath.Join(configured, filepath.FromSlash(parts[1]))
			}
		}
	}
	return ""
}

func packageHasEveDependency(packagePath string) bool {
	content, err := os.ReadFile(packagePath)
	if err != nil {
		return false
	}
	var manifest struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return false
	}
	return jsonTruthy(manifest.Dependencies["eve"]) || jsonTruthy(manifest.DevDependencies["eve"])
}

func jsonTruthy(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0
	default:
		return true
	}
}
