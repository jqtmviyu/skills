package main

import (
	"encoding/json"
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

type listOptions struct {
	global bool
	json   bool
	agents []agent.ID
}

type listResult struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Scope      string   `json:"scope"`
	Agents     []string `json:"agents"`
	Source     *string  `json:"source"`
	SourceURL  *string  `json:"sourceUrl"`
	SourceType *string  `json:"sourceType"`
	PluginName *string  `json:"-"`
}

func parseListArgs(args []string) (listOptions, error) {
	var options listOptions
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-g", "--global":
			options.global = true
		case "--json":
			options.json = true
		case "-a", "--agent":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, fmt.Errorf("%s requires an agent name", args[index])
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
			if strings.HasPrefix(args[index], "-") {
				return options, fmt.Errorf("unknown list option: %s", args[index])
			}
			return options, fmt.Errorf("list does not accept positional arguments: %s", args[index])
		}
	}
	return options, nil
}

func runList(args []string, stdout, stderr io.Writer) int {
	options, err := parseListArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "skills: %v\n", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "skills: resolve project directory: %v\n", err)
		return 1
	}
	installed, err := installer.ListInstalledSkills(installer.ListOptions{Global: options.global, CWD: cwd, AgentFilter: options.agents})
	if err != nil {
		fmt.Fprintf(stderr, "skills: list installed skills: %v\n", err)
		return 1
	}
	var locked map[string]lockfile.Entry
	if options.global {
		lock, readErr := lockfile.ReadGlobal()
		if readErr != nil {
			fmt.Fprintf(stderr, "skills: read global lock: %v\n", readErr)
			return 1
		}
		locked = lock.Skills
	} else {
		lock, readErr := lockfile.ReadProject(cwd)
		if readErr != nil {
			fmt.Fprintf(stderr, "skills: read project lock: %v\n", readErr)
			return 1
		}
		locked = lock.Skills
	}

	results := make([]listResult, 0, len(installed))
	configs := make(map[agent.ID]agent.Config)
	for _, config := range agent.Registry() {
		configs[config.ID] = config
	}
	for _, skill := range installed {
		result := listResult{Name: skill.Name, Path: skill.CanonicalPath, Scope: scopeName(options.global), Agents: []string{}}
		for _, id := range skill.Agents {
			if config, ok := configs[id]; ok {
				result.Agents = append(result.Agents, config.DisplayName)
			}
		}
		entry, ok := findLockEntry(locked, skill.Name)
		if ok {
			result.Source = lockStringPointer(entry, "source")
			result.SourceURL = lockStringPointer(entry, "sourceUrl")
			result.SourceType = lockStringPointer(entry, "sourceType")
			result.PluginName = lockStringPointer(entry, "pluginName")
		}
		results = append(results, result)
	}
	if options.json {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(results); err != nil {
			fmt.Fprintf(stderr, "skills: write list output: %v\n", err)
			return 1
		}
		return 0
	}
	if len(results) == 0 {
		label := "project"
		if options.global {
			label = "global"
		}
		fmt.Fprintf(stdout, "No %s skills found.\n", label)
		return 0
	}
	groups := make(map[string][]listResult)
	var groupNames []string
	for _, result := range results {
		group := ""
		if result.PluginName != nil {
			group = *result.PluginName
		}
		if _, exists := groups[group]; !exists {
			groupNames = append(groupNames, group)
		}
		groups[group] = append(groups[group], result)
	}
	sort.Strings(groupNames)
	if len(groups[""]) > 0 {
		groupNames = append(groupNames, "")
	}
	for _, group := range groupNames {
		if group != "" {
			fmt.Fprintf(stdout, "%s\n", titleCase(group))
		} else if len(groups) > 1 {
			fmt.Fprintln(stdout, "General")
		}
		for _, result := range groups[group] {
			source := "local"
			if result.Source != nil {
				source = *result.Source
			}
			fmt.Fprintf(stdout, "%s\t%s\tAgents: %s\tSource: %s\n", result.Name, filepath.Clean(result.Path), strings.Join(result.Agents, ", "), source)
		}
	}
	return 0
}

func findLockEntry(skills map[string]lockfile.Entry, name string) (lockfile.Entry, bool) {
	if entry, ok := skills[name]; ok {
		return entry, true
	}
	sanitized := agent.SanitizeInstallName(name)
	var match lockfile.Entry
	for key, entry := range skills {
		if agent.SanitizeInstallName(key) != sanitized {
			continue
		}
		if match != nil {
			return nil, false
		}
		match = entry
	}
	return match, match != nil
}

func lockStringPointer(entry lockfile.Entry, key string) *string {
	value, ok := entry.String(key)
	if !ok {
		return nil
	}
	return &value
}
