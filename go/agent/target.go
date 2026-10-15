package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const MissingAgentProjectDirectory = "missing-agent-project-directory"

// TargetOptions select the scope and project-specific Eve destination.
type TargetOptions struct {
	Global                 bool
	CWD                    string
	EveSubagent            string
	CreateMissingAgentRoot bool
}

// Target contains both the canonical installation directory and the selected
// agent directory. InstallDir is the default symlink-mode destination; copy
// mode writes directly to AgentDir instead.
type Target struct {
	AgentID       ID
	Global        bool
	Universal     bool
	InstallName   string
	CanonicalBase string
	CanonicalDir  string
	AgentBase     string
	AgentDir      string
	InstallDir    string
	SkipAgentLink bool
	SkipReason    string
}

// ResolveTarget computes safe installation paths for an agent and skill name.
func ResolveTarget(id ID, skillName string, options TargetOptions) (Target, error) {
	config, ok := ConfigFor(id)
	if !ok {
		return Target{}, fmt.Errorf("unknown agent %q", id)
	}
	if options.Global && !config.SupportsGlobal {
		return Target{}, fmt.Errorf("%s does not support global skill installation", config.DisplayName)
	}

	installName := SanitizeInstallName(skillName)
	cwd := options.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return Target{}, fmt.Errorf("resolve project directory: %w", err)
		}
	}
	var err error
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return Target{}, fmt.Errorf("resolve project directory %q: %w", cwd, err)
	}

	home := homeDirectory()
	base := cwd
	if options.Global {
		base = home
	}
	canonicalBase := filepath.Join(base, ".agents", "skills")
	universal := config.SkillsDir == UniversalSkillsDir

	var agentBase string
	switch {
	case universal:
		agentBase = canonicalBase
	case id == "eve" && options.EveSubagent != "":
		agentBase = filepath.Join(cwd, "agent", "subagents", SanitizeInstallName(options.EveSubagent), "skills")
	case options.Global:
		agentBase = config.GlobalSkillsDir
	default:
		agentBase = filepath.Join(cwd, filepath.FromSlash(config.SkillsDir))
	}
	if id == "eve" && options.EveSubagent == "" && !options.Global {
		agentBase = filepath.Join(cwd, "agent", "skills")
	}
	if id == "eve" && !options.Global {
		canonicalBase = agentBase
	}

	canonicalDir := filepath.Join(canonicalBase, installName)
	agentDir := filepath.Join(agentBase, installName)
	if !pathWithin(canonicalBase, canonicalDir) || !pathWithin(agentBase, agentDir) {
		return Target{}, fmt.Errorf("invalid skill name: potential path traversal detected")
	}

	target := Target{
		AgentID: id, Global: options.Global, Universal: universal,
		InstallName: installName, CanonicalBase: canonicalBase, CanonicalDir: canonicalDir,
		AgentBase: agentBase, AgentDir: agentDir, InstallDir: agentDir,
	}
	if !options.Global && !universal && canonicalBase != agentBase && !options.CreateMissingAgentRoot && !config.CreateProjectSkillsDirByDefault {
		root := strings.Split(config.SkillsDir, "/")[0]
		if !pathExists(filepath.Join(cwd, root)) {
			target.SkipAgentLink = true
			target.SkipReason = MissingAgentProjectDirectory
			target.InstallDir = canonicalDir
		}
	}
	if options.Global && universal {
		target.InstallDir = canonicalDir
	}
	return target, nil
}

// SanitizeInstallName converts untrusted skill or Eve-subagent names into
// kebab-case path components. It is deliberately separate from terminal text
// sanitization because path names and terminal output have different rules.
func SanitizeInstallName(name string) string {
	name = strings.ToLower(name)
	var result strings.Builder
	inReplacement := false
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' {
			result.WriteRune(character)
			inReplacement = false
			continue
		}
		if !inReplacement {
			result.WriteByte('-')
			inReplacement = true
		}
	}
	sanitized := strings.TrimFunc(result.String(), func(character rune) bool {
		return character == '.' || character == '-'
	})
	if len(sanitized) > 255 {
		sanitized = sanitized[:255]
	}
	if sanitized == "" {
		return "unnamed-skill"
	}
	return sanitized
}

func pathWithin(base, target string) bool {
	absoluteBase, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(absoluteBase, absoluteTarget)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
