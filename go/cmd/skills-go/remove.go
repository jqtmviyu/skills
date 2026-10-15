package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
	"github.com/vercel-labs/skills/go/installer"
	"github.com/vercel-labs/skills/go/lockfile"
)

type removeOptions struct {
	global bool
	yes    bool
	all    bool
	agents []agent.ID
	skills []string
}

func parseRemoveArgs(args []string) (removeOptions, error) {
	var options removeOptions
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "-g", "--global":
			options.global = true
		case "-y", "--yes":
			options.yes = true
		case "--all":
			options.all, options.yes = true, true
		case "-s", "--skill":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, fmt.Errorf("%s requires a skill name", arg)
			}
			options.skills = append(options.skills, values...)
			index = next - 1
		case "-a", "--agent":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, fmt.Errorf("%s requires an agent name", arg)
			}
			for _, value := range values {
				id := agent.ID(value)
				if _, ok := agent.ConfigFor(id); !ok {
					return options, fmt.Errorf("invalid agent %q", value)
				}
				options.agents = append(options.agents, id)
			}
			index = next - 1
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown remove option: %s", arg)
			}
			options.skills = append(options.skills, arg)
		}
	}
	if options.all && len(options.skills) > 0 && !containsString(options.skills, "*") {
		return options, fmt.Errorf("cannot combine --all with specific skill names")
	}
	if containsString(options.skills, "*") {
		options.all, options.yes = true, true
		options.skills = nil
	}
	return options, nil
}

func runRemove(args []string, stdout, stderr io.Writer) int {
	options, err := parseRemoveArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "skills-go: %v\n", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "skills-go: resolve project directory: %v\n", err)
		return 1
	}
	lockSkills, lock, err := readManagementLock(options.global, cwd)
	if err != nil {
		fmt.Fprintf(stderr, "skills-go: read lock: %v\n", err)
		return 1
	}
	installed, err := installer.ListInstalledSkills(installer.ListOptions{Global: options.global, CWD: cwd})
	if err != nil {
		fmt.Fprintf(stderr, "skills-go: list installed skills: %v\n", err)
		return 1
	}
	resolved := resolveRemovalNames(options.skills, options.all, installed, lockSkills)
	if len(resolved) == 0 {
		if !options.all && len(options.skills) > 0 {
			fmt.Fprintf(stderr, "skills-go: no matching skills found for: %s\n", strings.Join(options.skills, ", "))
			return 0
		}
		fmt.Fprintln(stdout, "No skills found to remove.")
		return 0
	}
	if len(options.skills) == 0 && !options.all {
		if !isTerminal(os.Stdin) {
			fmt.Fprintln(stderr, "skills-go: specify skill names or use --all when running non-interactively")
			return 1
		}
		resolved, err = promptRemoveSkills(os.Stdin, stdout, resolved)
		if err != nil {
			fmt.Fprintf(stderr, "skills-go: %v\n", err)
			return 1
		}
		if len(resolved) == 0 {
			fmt.Fprintln(stdout, "Removal cancelled.")
			return 0
		}
	}

	targetAgents := options.agents
	if len(targetAgents) == 0 {
		for _, config := range agent.Registry() {
			targetAgents = append(targetAgents, config.ID)
		}
	}
	for _, id := range targetAgents {
		config, _ := agent.ConfigFor(id)
		if options.global && !config.SupportsGlobal {
			if len(options.agents) > 0 {
				fmt.Fprintf(stderr, "skills-go: %s does not support global skill removal\n", config.DisplayName)
				return 1
			}
		}
	}
	if !options.yes {
		if !isTerminal(os.Stdin) {
			fmt.Fprintln(stdout, "Removal skipped in non-interactive mode; use --yes to confirm.")
			return 0
		}
		fmt.Fprintf(stdout, "Remove %d skill(s)? [y/N] ", len(resolved))
		answer, readErr := readPromptLine(os.Stdin)
		if readErr != nil {
			fmt.Fprintf(stderr, "skills-go: read removal confirmation: %v\n", readErr)
			return 1
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(stdout, "Removal cancelled.")
			return 0
		}
	}

	changedLock := false
	failed := false
	for _, name := range resolved {
		for _, id := range targetAgents {
			config, _ := agent.ConfigFor(id)
			if options.global && !config.SupportsGlobal {
				continue
			}
			_, removeErr := installer.RemoveAgentSkill(name, id, installer.Options{Global: options.global, CWD: cwd})
			if removeErr != nil {
				fmt.Fprintf(stderr, "skills-go: remove %s for %s: %v\n", name, id, removeErr)
				failed = true
			}
			if id == "eve" && !options.global {
				for _, subagent := range agent.EveSubagents(cwd) {
					_, removeErr = installer.RemoveAgentSkill(name, id, installer.Options{CWD: cwd, EveSubagent: subagent})
					if removeErr != nil {
						fmt.Fprintf(stderr, "skills-go: remove %s for eve/%s: %v\n", name, subagent, removeErr)
						failed = true
					}
				}
			}
		}

		remaining := remainingAgentsUseSkill(name, targetAgents, options.global, cwd)
		if !remaining {
			if err := installer.RemoveCanonicalSkill(name, installer.Options{Global: options.global, CWD: cwd}); err != nil {
				fmt.Fprintf(stderr, "skills-go: remove canonical %s: %v\n", name, err)
				failed = true
			}
			if _, exists := lockSkills[name]; exists {
				delete(lockSkills, name)
				changedLock = true
			} else {
				for key := range lockSkills {
					if agent.SanitizeInstallName(key) == agent.SanitizeInstallName(name) {
						delete(lockSkills, key)
						changedLock = true
					}
				}
			}
		}
	}
	if changedLock {
		err = writeManagementLock(options.global, cwd, lock)
		if err != nil {
			fmt.Fprintf(stderr, "skills-go: write lock: %v\n", err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	fmt.Fprintf(stdout, "Removed %d skill(s).\n", len(resolved))
	return 0
}

func resolveRemovalNames(requested []string, all bool, installed []installer.InstalledSkill, lockSkills map[string]lockfile.Entry) []string {
	bySanitized := make(map[string]map[string]bool)
	installedExact := make(map[string]bool)
	for key := range lockSkills {
		addRemovalCandidate(bySanitized, key)
	}
	for _, skill := range installed {
		installedExact[skill.Name] = true
		addRemovalCandidate(bySanitized, skill.Name)
	}
	if all {
		var result []string
		for _, candidates := range bySanitized {
			for name := range candidates {
				result = append(result, name)
			}
		}
		sort.Strings(result)
		return result
	}
	var result []string
	seen := make(map[string]bool)
	for _, name := range requested {
		if _, exactLockKey := lockSkills[name]; exactLockKey || installedExact[name] {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
			continue
		}
		sanitized := agent.SanitizeInstallName(name)
		candidates := bySanitized[sanitized]
		if len(candidates) == 1 {
			for candidate := range candidates {
				if !seen[candidate] {
					seen[candidate] = true
					result = append(result, candidate)
				}
			}
			continue
		}
		if len(candidates) > 1 {
			continue
		}
		for key, entry := range lockSkills {
			source, _ := entry.String("source")
			if source == name && !seen[key] {
				seen[key] = true
				result = append(result, key)
			}
		}
	}
	return result
}

func addRemovalCandidate(bySanitized map[string]map[string]bool, name string) {
	key := agent.SanitizeInstallName(name)
	if bySanitized[key] == nil {
		bySanitized[key] = make(map[string]bool)
	}
	bySanitized[key][name] = true
}

func remainingAgentsUseSkill(name string, selected []agent.ID, global bool, cwd string) bool {
	selectedSet := make(map[agent.ID]bool)
	for _, id := range selected {
		selectedSet[id] = true
	}
	detected := make(map[agent.ID]bool)
	for _, id := range agent.DetectInstalled(cwd) {
		detected[id] = true
	}
	for _, config := range agent.Registry() {
		id := config.ID
		if selectedSet[id] || (global && !config.SupportsGlobal) {
			continue
		}
		if agent.IsUniversal(id) && !detected[id] {
			continue
		}
		target, err := agent.ResolveTarget(id, name, agent.TargetOptions{Global: global, CWD: cwd})
		if err == nil && hasSkillMarkdown(target.AgentDir) {
			return true
		}
		if id == "eve" && !global {
			for _, subagent := range agent.EveSubagents(cwd) {
				target, err := agent.ResolveTarget(id, name, agent.TargetOptions{CWD: cwd, EveSubagent: subagent})
				if err == nil && hasSkillMarkdown(target.AgentDir) {
					return true
				}
			}
		}
	}
	return false
}

func hasSkillMarkdown(directory string) bool {
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(directory, "SKILL.md"))
	return err == nil
}

func promptRemoveSkills(input io.Reader, output io.Writer, skills []string) ([]string, error) {
	fmt.Fprintln(output, "Select skills to remove:")
	for index, skill := range skills {
		fmt.Fprintf(output, "  %d) %s\n", index+1, skill)
	}
	fmt.Fprint(output, "Skill numbers (comma-separated, or * for all): ")
	var line string
	if _, err := fmt.Fscanln(bufio.NewReader(input), &line); err != nil {
		return nil, fmt.Errorf("read skill selection: %w", err)
	}
	if line == "*" {
		return skills, nil
	}
	var selected []string
	for _, part := range strings.Split(line, ",") {
		index := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &index); err != nil || index < 1 || index > len(skills) {
			return nil, fmt.Errorf("invalid skill selection %q", part)
		}
		selected = append(selected, skills[index-1])
	}
	return selected, nil
}
