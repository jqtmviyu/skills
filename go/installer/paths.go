package installer

import (
	"os"

	"github.com/vercel-labs/skills/go/agent"
)

// GetInstallPath returns the selected agent's skill directory, even when an
// automatic project install will use the canonical directory instead.
func GetInstallPath(skillName string, agentID agent.ID, options Options) (string, error) {
	target, err := resolveTarget(skillName, agentID, options)
	if err != nil {
		return "", err
	}
	return target.AgentDir, nil
}

// GetCanonicalPath returns the path used as the shared source of truth for
// symlink-mode installs. Eve uses its root or subagent skill directory.
func GetCanonicalPath(skillName string, agentID agent.ID, options Options) (string, error) {
	target, err := resolveTarget(skillName, agentID, options)
	if err != nil {
		return "", err
	}
	return target.CanonicalDir, nil
}

// IsSkillInstalled reports whether the selected target currently exists.
func IsSkillInstalled(skillName string, agentID agent.ID, options Options) bool {
	target, err := resolveTarget(skillName, agentID, options)
	if err != nil {
		return false
	}
	_, err = os.Stat(target.AgentDir)
	return err == nil
}

func resolveTarget(skillName string, agentID agent.ID, options Options) (agent.Target, error) {
	return agent.ResolveTarget(agentID, skillName, agent.TargetOptions{
		Global:                 options.Global,
		CWD:                    options.CWD,
		EveSubagent:            options.EveSubagent,
		CreateMissingAgentRoot: options.CreateMissingAgentRoot,
	})
}
