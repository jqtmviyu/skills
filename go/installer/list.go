package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
	"github.com/vercel-labs/skills/go/skillscan"
)

// ListOptions selects the installation scope and optional agent subset.
type ListOptions struct {
	Global      bool
	CWD         string
	AgentFilter []agent.ID
}

// InstalledSkill describes one skill installation grouped across its agent targets.
type InstalledSkill struct {
	Name          string
	Path          string
	CanonicalPath string
	Agents        []agent.ID
}

// ListInstalledSkills inspects canonical and agent-specific install directories.
// It reports each skill once per scope and attributes only directories that exist.
func ListInstalledSkills(options ListOptions) ([]InstalledSkill, error) {
	cwd := options.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve project directory: %w", err)
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve project directory %q: %w", cwd, err)
	}
	allowed := make(map[agent.ID]bool)
	for _, id := range options.AgentFilter {
		allowed[id] = true
	}
	filtered := len(options.AgentFilter) > 0
	detected := make(map[agent.ID]bool)
	for _, id := range agent.DetectInstalled(cwd) {
		detected[id] = true
	}
	canonical := filepath.Join(cwd, ".agents", "skills")
	if options.Global {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, fmt.Errorf("resolve home directory: %w", homeErr)
		}
		canonical = filepath.Join(home, ".agents", "skills")
	}

	type directoryTarget struct {
		path  string
		agent agent.ID
	}
	targets := []directoryTarget{{path: canonical}}
	for _, config := range agent.Registry() {
		if filtered && !allowed[config.ID] {
			continue
		}
		if options.Global && !config.SupportsGlobal {
			continue
		}
		if agent.IsUniversal(config.ID) && !detected[config.ID] {
			continue
		}
		target, targetErr := agent.ResolveTarget(config.ID, "skills-go-list-probe", agent.TargetOptions{Global: options.Global, CWD: cwd})
		if targetErr == nil {
			targets = append(targets, directoryTarget{path: target.AgentBase, agent: config.ID})
		}
		if config.ID == "eve" && !options.Global {
			for _, subagent := range agent.EveSubagents(cwd) {
				subagentTarget, subagentErr := agent.ResolveTarget(config.ID, "skills-go-list-probe", agent.TargetOptions{CWD: cwd, EveSubagent: subagent})
				if subagentErr == nil {
					targets = append(targets, directoryTarget{path: subagentTarget.AgentBase, agent: config.ID})
				}
			}
		}
	}

	byName := make(map[string]*InstalledSkill)
	for _, target := range targets {
		entries, readErr := os.ReadDir(target.path)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read installed skills directory %q: %w", target.path, readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			directory := filepath.Join(target.path, entry.Name())
			info, statErr := os.Stat(directory)
			if statErr != nil || !info.IsDir() {
				continue
			}
			content, readErr := os.ReadFile(filepath.Join(directory, "SKILL.md"))
			if readErr != nil {
				continue
			}
			name, _, _, valid := skillscan.ParseSkillMetadata(string(content))
			if !valid {
				continue
			}
			key := strings.ToLower(name)
			item := byName[key]
			if item == nil {
				item = &InstalledSkill{Name: name, Path: directory, CanonicalPath: directory}
				byName[key] = item
			}
			if target.agent != "" && !containsInstalledAgent(item.Agents, target.agent) {
				item.Agents = append(item.Agents, target.agent)
			}
		}
	}

	result := make([]InstalledSkill, 0, len(byName))
	for _, item := range byName {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func containsInstalledAgent(agents []agent.ID, id agent.ID) bool {
	for _, existing := range agents {
		if existing == id {
			return true
		}
	}
	return false
}
