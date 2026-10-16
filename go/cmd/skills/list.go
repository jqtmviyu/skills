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
	writeListText(stdout, results, options.global, cwd, listColorEnabled(stdout))
	return 0
}

func writeListText(output io.Writer, results []listResult, global bool, cwd string, colored bool) {
	scope := "Project"
	if global {
		scope = "Global"
	}
	if len(results) == 0 {
		fmt.Fprintln(output, listPaint("No "+strings.ToLower(scope)+" skills found.", "\x1b[38;5;102m", colored))
		if global {
			fmt.Fprintln(output, listPaint("Try listing project skills without -g", "\x1b[38;5;102m", colored))
		} else {
			fmt.Fprintln(output, listPaint("Try listing global skills with -g", "\x1b[38;5;102m", colored))
		}
		return
	}

	fmt.Fprintln(output, listPaint(scope+" Skills", "\x1b[1m", colored))
	fmt.Fprintln(output)

	grouped := make(map[string][]listResult)
	ungrouped := make([]listResult, 0, len(results))
	var groupNames []string
	for _, result := range results {
		group := ""
		if result.PluginName != nil {
			group = sanitizeListCell(*result.PluginName)
		}
		if group == "" {
			ungrouped = append(ungrouped, result)
			continue
		}
		if _, exists := grouped[group]; !exists {
			groupNames = append(groupNames, group)
		}
		grouped[group] = append(grouped[group], result)
	}
	sort.Strings(groupNames)
	hasGroups := len(groupNames) > 0
	for _, group := range groupNames {
		fmt.Fprintln(output, listPaint(titleCase(group), "\x1b[1m", colored))
		writeListRows(output, grouped[group], "  ", cwd, colored)
		fmt.Fprintln(output)
	}
	if len(ungrouped) > 0 {
		indent := ""
		if hasGroups {
			fmt.Fprintln(output, listPaint("General", "\x1b[1m", colored))
			indent = "  "
		}
		writeListRows(output, ungrouped, indent, cwd, colored)
		fmt.Fprintln(output)
	}
}

func writeListRows(output io.Writer, results []listResult, indent, cwd string, colored bool) {
	home, _ := os.UserHomeDir()
	maxNameLength, maxPathLength := 0, 0
	names, paths := make([]string, len(results)), make([]string, len(results))
	for index, result := range results {
		names[index] = sanitizeListCell(result.Name)
		paths[index] = shortenListPath(result.Path, cwd, home)
		if length := len([]rune(names[index])); length > maxNameLength {
			maxNameLength = length
		}
		if length := len([]rune(paths[index])); length > maxPathLength {
			maxPathLength = length
		}
	}
	for index, result := range results {
		name := padListCell(names[index], maxNameLength)
		path := padListCell(paths[index], maxPathLength)
		agents := formatListAgents(result.Agents, colored)
		source := "local"
		if result.Source != nil {
			source = sanitizeListCell(*result.Source)
		}
		fmt.Fprintf(output, "%s%s %s\n", indent, listPaint(name, "\x1b[36m", colored), listPaint(path, "\x1b[38;5;102m", colored))
		fmt.Fprintf(output, "%s  %s %s  %s %s\n", indent, listPaint("Agents:", "\x1b[38;5;102m", colored), agents, listPaint("Source:", "\x1b[38;5;102m", colored), source)
	}
}

func formatListAgents(agents []string, colored bool) string {
	if len(agents) == 0 {
		return listPaint("not linked", "\x1b[33m", colored)
	}
	labels := make([]string, 0, len(agents))
	limit := len(agents)
	if limit > 5 {
		limit = 5
	}
	for _, agent := range agents[:limit] {
		labels = append(labels, sanitizeListCell(agent))
	}
	formatted := strings.Join(labels, ", ")
	if remaining := len(agents) - limit; remaining > 0 {
		formatted += fmt.Sprintf(" +%d more", remaining)
	}
	return formatted
}

func listPaint(value, code string, enabled bool) string {
	if !enabled {
		return value
	}
	return code + value + "\x1b[0m"
}

func listColorEnabled(output io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if force := os.Getenv("FORCE_COLOR"); force != "" && force != "0" {
		return true
	}
	file, ok := output.(*os.File)
	return ok && isTerminal(file)
}

func shortenListPath(fullPath, cwd, home string) string {
	fullPath = filepath.Clean(fullPath)
	for _, root := range []struct {
		path   string
		prefix string
	}{{home, "~"}, {cwd, "."}} {
		if root.path == "" {
			continue
		}
		relative, err := filepath.Rel(root.path, fullPath)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			continue
		}
		if relative == "." {
			return root.prefix
		}
		return filepath.Join(root.prefix, relative)
	}
	return fullPath
}

func sanitizeListCell(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
}

func padListCell(value string, width int) string {
	if length := len([]rune(value)); length < width {
		return value + strings.Repeat(" ", width-length)
	}
	return value
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
