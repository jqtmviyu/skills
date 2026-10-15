package installer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vercel-labs/skills/go/agent"
)

// RemoveAgentSkill removes one agent-specific copy or link. Shared universal
// directories are preserved for the caller to remove after checking remaining users.
func RemoveAgentSkill(skillName string, agentID agent.ID, options Options) (string, error) {
	target, err := resolveTarget(skillName, agentID, options)
	if err != nil {
		return "", err
	}
	if agent.IsUniversal(agentID) && samePath(target.AgentDir, target.CanonicalDir) {
		return target.AgentDir, nil
	}
	if err := os.RemoveAll(target.AgentDir); err != nil {
		return target.AgentDir, fmt.Errorf("remove installed skill %q for %s: %w", skillName, agentID, err)
	}
	return target.AgentDir, nil
}

// RemoveCanonicalSkill removes the shared project/global installation directory.
func RemoveCanonicalSkill(skillName string, options Options) error {
	target, err := agent.ResolveTarget(agent.UniversalID, skillName, agent.TargetOptions{Global: options.Global, CWD: options.CWD})
	if err != nil {
		return err
	}
	if !pathWithin(target.CanonicalBase, target.CanonicalDir) {
		return fmt.Errorf("refusing to remove unsafe canonical skill path %q", target.CanonicalDir)
	}
	if err := os.RemoveAll(filepath.Clean(target.CanonicalDir)); err != nil {
		return fmt.Errorf("remove canonical skill %q: %w", skillName, err)
	}
	return nil
}
