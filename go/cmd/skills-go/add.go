package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vercel-labs/skills/go/agent"
	"github.com/vercel-labs/skills/go/installer"
	"github.com/vercel-labs/skills/go/lockfile"
	"github.com/vercel-labs/skills/go/skillscan"
)

type addOptions struct {
	global    bool
	globalSet bool
	agents    []string
	skills    []string
	subagents []string
	yes       bool
	list      bool
	all       bool
	fullDepth bool
	copy      bool
	json      bool
	metadata  string
}

type addInstallTarget struct {
	Agent    agent.ID
	Subagent string
	Label    string
}

type addJSONResult struct {
	Name     string   `json:"name,omitempty"`
	Status   string   `json:"status"`
	Source   string   `json:"source,omitempty"`
	Ref      *string  `json:"ref"`
	Hash     *string  `json:"hash"`
	Path     string   `json:"path,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Agents   []string `json:"agents,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	Security any      `json:"security"`
	Reason   string   `json:"reason,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func runAdd(args []string, stdout, stderr io.Writer) int {
	options, source, err := parseAddArgs(args)
	if err != nil {
		return addFailure(stdout, stderr, options.json, err.Error())
	}
	if source == "" {
		return addFailure(stdout, stderr, options.json, "add requires a source")
	}
	if options.json && !options.yes {
		return addFailure(stdout, stderr, true, "the --json flag requires --yes (or --all) to run non-interactively")
	}
	if options.json && options.list {
		return addFailure(stdout, stderr, true, "the --json flag cannot be combined with --list")
	}

	ctx := context.Background()
	includeInternal := hasSpecificSkillSelector(options.skills)
	if parsed, parseErr := skillscan.ParseSource(source); parseErr == nil && parsed.SkillFilter != "" {
		includeInternal = true
	}
	session, err := openAddSource(ctx, source, skillscan.Options{FullDepth: options.fullDepth, IncludeInternal: includeInternal})
	if err != nil {
		return addFailure(stdout, stderr, options.json, err.Error())
	}
	defer session.Close()
	scan := session.scan
	sourceInfo := session.source
	if status := scan.FilterStatus(); status.Message != "" {
		fmt.Fprintf(stderr, "Warning: %s\n", status.Message)
	}

	candidates := scan.Candidates()
	if len(candidates) == 0 {
		return addFailure(stdout, stderr, options.json, "no valid skills found. Skills require a SKILL.md with name and description.")
	}
	if options.list {
		printAvailableSkills(stdout, candidates)
		return 0
	}

	selected, skipped, err := selectAddSkills(candidates, sourceInfo.SkillFilter, options, os.Stdin, stdout)
	if err != nil {
		return addFailure(stdout, stderr, options.json, err.Error())
	}
	if len(skipped) > 0 {
		fmt.Fprintf(stderr, "skills-go: no matching skills found for: %s\n", strings.Join(skipped, ", "))
	}
	if len(selected) == 0 {
		if options.json {
			results := make([]addJSONResult, 0, len(skipped))
			for _, name := range skipped {
				results = append(results, addJSONResult{Name: name, Status: "skipped", Reason: "No matching skill found in source", Security: nil})
			}
			writeJSON(stdout, results)
			return 1
		}
		return 1
	}
	if !options.yes && !isTerminal(os.Stdin) {
		return addFailure(stdout, stderr, options.json, "interactive prompt required; use --yes to install non-interactively")
	}

	targets, explicitAgents, err := selectAgents(options, os.Stdin, stdout)
	if err != nil {
		return addFailure(stdout, stderr, options.json, err.Error())
	}
	if len(options.subagents) > 0 && !containsAgent(targets, "eve") {
		targets = append(targets, "eve")
		explicitAgents = true
	}
	if containsAgent(targets, "eve") && len(options.subagents) == 0 && !options.yes && isTerminal(os.Stdin) {
		cwdForPrompt, _ := os.Getwd()
		options.subagents, err = promptEveSubagents(cwdForPrompt, os.Stdin, stdout)
		if err != nil {
			return addFailure(stdout, stderr, options.json, err.Error())
		}
	}
	if len(targets) == 0 {
		return addFailure(stdout, stderr, options.json, "no agents selected")
	}
	installTargets := expandInstallTargets(targets, options.subagents)
	if !options.yes && isTerminal(os.Stdin) && !options.globalSet {
		options.global, err = promptInstallScope(targets, os.Stdin, stdout)
		if err != nil {
			return addFailure(stdout, stderr, options.json, err.Error())
		}
	}
	if options.global {
		for _, target := range targets {
			config, _ := agent.ConfigFor(target)
			if !config.SupportsGlobal {
				return addFailure(stdout, stderr, options.json, fmt.Sprintf("%s does not support global skill installation", config.DisplayName))
			}
		}
	}
	mode := selectInstallMode(targets, options)
	if !options.yes && isTerminal(os.Stdin) {
		mode, err = promptInstallMode(targets, options.global, mode, os.Stdin, stdout)
		if err != nil {
			return addFailure(stdout, stderr, options.json, err.Error())
		}
		confirmed, confirmErr := promptInstallConfirmation(os.Stdin, stdout)
		if confirmErr != nil {
			return addFailure(stdout, stderr, options.json, confirmErr.Error())
		}
		if !confirmed {
			fmt.Fprintln(stdout, "Installation cancelled")
			return 0
		}
	}

	selectedPaths := make([]string, 0, len(selected))
	for _, candidate := range selected {
		selectedPaths = append(selectedPaths, candidate.Path)
	}
	materialized, err := scan.Materialize(selectedPaths)
	if err != nil {
		return addFailure(stdout, stderr, options.json, err.Error())
	}
	byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
	for _, skill := range materialized {
		byPath[skill.Candidate.Path] = skill
	}

	results := make([]addJSONResult, 0, len(selected)+len(skipped))
	failed := false
	cwd, _ := os.Getwd()
	for _, requested := range skipped {
		results = append(results, addJSONResult{Name: requested, Status: "skipped", Reason: "No matching skill found in source", Security: nil})
	}
	for _, candidate := range selected {
		skillSource := byPath[candidate.Path]
		installedNames := make([]string, 0, len(targets))
		var firstResult installer.Result
		var firstError error
		for _, target := range installTargets {
			result, installErr := installer.InstallSkill(installer.Skill{
				Name: candidate.Name, Path: skillSource.Directory, PluginName: candidate.PluginName,
			}, target.Agent, installer.Options{
				Global: options.global, CWD: cwd, Mode: mode, EveSubagent: target.Subagent,
				CreateMissingAgentRoot: explicitAgents, Warnings: stderr,
			})
			if firstResult.Path == "" {
				firstResult = result
			}
			if installErr != nil {
				failed = true
				if firstError == nil {
					firstError = installErr
				}
				fmt.Fprintf(stderr, "Failed to install %s for %s: %v\n", candidate.Name, target.Label, installErr)
				continue
			}
			if result.Success && !result.Skipped {
				installedNames = append(installedNames, target.Label)
			}
		}

		hash, hashErr := lockfile.ComputeProjectHash(skillSource.Directory)
		if hashErr != nil {
			fmt.Fprintf(stderr, "Warning: cannot hash %s: %v\n", candidate.Name, hashErr)
		}
		treeHash, _ := scan.GitTreeHash(candidate.Path)
		wellKnownDigest := ""
		if session.wellKnown {
			wellKnownDigest = session.wellKnownDigests[candidate.Name]
			if wellKnownDigest == "" {
				wellKnownDigest, hashErr = computeWellKnownDigest(skillSource.Directory)
			}
			if hashErr != nil {
				fmt.Fprintf(stderr, "Warning: cannot compute well-known digest for %s: %v\n", candidate.Name, hashErr)
			}
		}
		if len(installedNames) > 0 && !session.direct {
			if err := writeAddLock(options.global, cwd, sourceInfo, candidate, hash, treeHash, session.wellKnown, wellKnownDigest, eveSubagentValues(installTargets)); err != nil {
				fmt.Fprintf(stderr, "Warning: cannot update skill lock: %v\n", err)
			}
		}
		if firstError != nil {
			results = append(results, addJSONResult{Name: candidate.Name, Status: "failed", Error: firstError.Error(), Security: nil})
			continue
		}
		if options.json {
			sourceValue := jsonSource(sourceInfo)
			if session.direct {
				sourceValue = sourceInfo.URL
			}
			ref := sourceInfo.Ref
			var refValue *string
			if ref != "" {
				refValue = &ref
			}
			hashValue := hash
			results = append(results, addJSONResult{
				Name: candidate.Name, Status: "installed", Source: sourceValue, Ref: refValue,
				Hash: &hashValue, Path: installResultPath(firstResult),
				Scope: scopeName(options.global), Agents: installedNames, Mode: string(firstResult.Mode), Security: nil,
			})
		} else {
			fmt.Fprintf(stdout, "Installed %s (%d agent target(s)) at %s\n", candidate.Name, len(installedNames), installResultPath(firstResult))
		}
	}
	if options.json {
		writeJSON(stdout, results)
	} else {
		fmt.Fprintln(stdout, "Done! Review skills before use; they run with full agent permissions.")
	}
	if failed || len(skipped) > 0 {
		return 1
	}
	return 0
}

func installResultPath(result installer.Result) string {
	if result.CanonicalPath != "" {
		return result.CanonicalPath
	}
	return result.Path
}

func parseAddArgs(args []string) (addOptions, string, error) {
	options := addOptions{json: containsString(args, "--json"), all: containsString(args, "--all")}
	options.yes = options.all
	var source string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			return options, "", errors.New("help requested")
		case "-g", "--global":
			options.global, options.globalSet = true, true
		case "-p", "--project":
			options.global, options.globalSet = false, true
		case "-y", "--yes":
			options.yes = true
		case "-l", "--list":
			options.list = true
		case "--all":
			options.all = true
		case "--full-depth":
			options.fullDepth = true
		case "--copy":
			options.copy = true
		case "--json":
			options.json = true
		case "-a", "--agent":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, source, fmt.Errorf("%s requires an agent name", arg)
			}
			options.agents = append(options.agents, values...)
			index = next - 1
		case "-s", "--skill":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, source, fmt.Errorf("%s requires a skill name", arg)
			}
			options.skills = append(options.skills, values...)
			index = next - 1
		case "--subagent":
			values, next := consumeValues(args, index+1)
			if len(values) == 0 {
				return options, source, errors.New("--subagent requires a name")
			}
			options.subagents = append(options.subagents, values...)
			index = next - 1
		case "--metadata":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return options, source, errors.New("--metadata requires a JSON value")
			}
			index++
			if !json.Valid([]byte(args[index])) {
				return options, source, errors.New("--metadata must be valid JSON")
			}
			options.metadata = args[index]
		default:
			if strings.HasPrefix(arg, "-") {
				return options, source, fmt.Errorf("unknown option: %s", arg)
			}
			if source != "" {
				return options, source, fmt.Errorf("expected one source, received multiple arguments: %s", arg)
			}
			source = arg
		}
	}
	if options.all {
		options.skills = []string{"*"}
		options.agents = []string{"*"}
		options.yes = true
	}
	return options, source, nil
}

func consumeValues(args []string, start int) ([]string, int) {
	var values []string
	index := start
	for index < len(args) && !strings.HasPrefix(args[index], "-") {
		values = append(values, args[index])
		index++
	}
	return values, index
}

func selectAddSkills(candidates []skillscan.Candidate, sourceSelector string, options addOptions, input io.Reader, output io.Writer) ([]skillscan.Candidate, []string, error) {
	selectors := append([]string(nil), options.skills...)
	if sourceSelector != "" && !containsString(selectors, sourceSelector) {
		selectors = append(selectors, sourceSelector)
	}
	if containsString(selectors, "*") {
		return candidates, nil, nil
	}
	if len(selectors) > 0 {
		var selected []skillscan.Candidate
		var skipped []string
		for _, selector := range selectors {
			matches := matchingCandidates(candidates, selector)
			if len(matches) == 0 {
				skipped = append(skipped, selector)
				continue
			}
			for _, candidate := range matches {
				if !containsCandidate(selected, candidate.Path) {
					selected = append(selected, candidate)
				}
			}
		}
		return selected, skipped, nil
	}
	if len(candidates) == 1 || options.yes {
		return candidates, nil, nil
	}
	if !isTerminal(os.Stdin) {
		return nil, nil, errors.New("multiple skills found; specify --skill <name> or use --yes to install all")
	}
	return promptForSkillSelection(candidates, input, output)
}

func matchingCandidates(candidates []skillscan.Candidate, selector string) []skillscan.Candidate {
	var matches []skillscan.Candidate
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Name, selector) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func promptForSkillSelection(candidates []skillscan.Candidate, input io.Reader, output io.Writer) ([]skillscan.Candidate, []string, error) {
	fmt.Fprintln(output, "Available skills:")
	for index, candidate := range candidates {
		fmt.Fprintf(output, "  %d) %s — %s\n", index+1, candidate.Name, candidate.Description)
	}
	fmt.Fprint(output, "Select skills by number (comma-separated, or * for all): ")
	var line string
	if _, err := fmt.Fscanln(input, &line); err != nil {
		return nil, nil, errors.New("skill selection cancelled")
	}
	if line == "*" {
		return candidates, nil, nil
	}
	var selected []skillscan.Candidate
	for _, item := range strings.Split(line, ",") {
		index := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(item), "%d", &index); err != nil || index < 1 || index > len(candidates) {
			return nil, nil, fmt.Errorf("invalid skill selection %q", item)
		}
		selected = append(selected, candidates[index-1])
	}
	if len(selected) == 0 {
		return nil, nil, errors.New("skill selection cancelled")
	}
	return selected, nil, nil
}

func selectAgents(options addOptions, input io.Reader, output io.Writer) ([]agent.ID, bool, error) {
	registry := agent.Registry()
	valid := make(map[string]bool, len(registry))
	for _, config := range registry {
		valid[string(config.ID)] = true
	}
	if len(options.agents) > 0 {
		if containsString(options.agents, "*") {
			if len(options.agents) != 1 {
				return nil, true, errors.New("--agent '*' cannot be combined with agent names")
			}
			result := make([]agent.ID, 0, len(registry))
			for _, config := range registry {
				result = append(result, config.ID)
			}
			return result, true, nil
		}
		var result []agent.ID
		seen := make(map[agent.ID]bool)
		for _, value := range options.agents {
			id := agent.ID(value)
			if !valid[value] {
				return nil, true, fmt.Errorf("invalid agent %q; valid agents: %s", value, strings.Join(registryNames(registry), ", "))
			}
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
		return result, true, nil
	}

	cwd, _ := os.Getwd()
	detected := agent.DetectInstalled(cwd)
	if len(detected) == 0 {
		if options.yes {
			result := make([]agent.ID, 0, len(registry))
			for _, config := range registry {
				result = append(result, config.ID)
			}
			return result, false, nil
		}
		if !isTerminal(os.Stdin) {
			return nil, false, errors.New("no agents detected; specify --agent <name> or --agent '*' and use --yes")
		}
		return promptForAgentSelection(registry, input, output)
	}
	if len(detected) > 1 && !options.yes && isTerminal(os.Stdin) {
		return promptDetectedAgentSelection(detected, registry, input, output, options.global)
	}
	selected := append([]agent.ID(nil), detected...)
	seen := make(map[agent.ID]bool, len(selected))
	for _, id := range selected {
		seen[id] = true
	}
	for _, id := range agent.UniversalAgents() {
		if !seen[id] {
			selected = append(selected, id)
			seen[id] = true
		}
	}
	return selected, false, nil
}

func promptDetectedAgentSelection(detected []agent.ID, registry []agent.Config, input io.Reader, output io.Writer, global bool) ([]agent.ID, bool, error) {
	detectedSet := make(map[agent.ID]bool, len(detected))
	for _, id := range detected {
		detectedSet[id] = true
	}
	var choices []agent.Config
	for _, config := range registry {
		if !detectedSet[config.ID] || config.SkillsDir == agent.UniversalSkillsDir {
			continue
		}
		if global && !config.SupportsGlobal {
			continue
		}
		choices = append(choices, config)
	}
	if len(choices) == 0 {
		return agent.UniversalAgents(), false, nil
	}
	selected, _, err := promptForAgentSelection(choices, input, output)
	if err != nil {
		return nil, true, err
	}
	seen := make(map[agent.ID]bool, len(selected))
	for _, id := range selected {
		seen[id] = true
	}
	for _, id := range agent.UniversalAgents() {
		if !seen[id] {
			selected = append(selected, id)
			seen[id] = true
		}
	}
	return selected, true, nil
}

func promptForAgentSelection(registry []agent.Config, input io.Reader, output io.Writer) ([]agent.ID, bool, error) {
	fmt.Fprintln(output, "Select agents:")
	for index, config := range registry {
		fmt.Fprintf(output, "  %d) %s (%s)\n", index+1, config.DisplayName, config.ID)
	}
	fmt.Fprint(output, "Agent numbers (comma-separated, or * for all): ")
	var line string
	if _, err := fmt.Fscanln(input, &line); err != nil {
		return nil, true, errors.New("agent selection cancelled")
	}
	if line == "*" {
		result := make([]agent.ID, 0, len(registry))
		for _, config := range registry {
			result = append(result, config.ID)
		}
		return result, true, nil
	}
	var result []agent.ID
	for _, item := range strings.Split(line, ",") {
		index := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(item), "%d", &index); err != nil || index < 1 || index > len(registry) {
			return nil, true, fmt.Errorf("invalid agent selection %q", item)
		}
		result = append(result, registry[index-1].ID)
	}
	return result, true, nil
}

func writeAddLock(global bool, cwd string, source skillscan.Source, candidate skillscan.Candidate, hash, treeHash string, wellKnown bool, wellKnownDigest string, subagents []string) error {
	sourceName, sourceType := lockSource(source), lockSourceType(source)
	if wellKnown {
		sourceName, sourceType = wellKnownSourceIdentifier(source.URL), "well-known"
	}
	if global {
		lock, err := lockfile.ReadGlobal()
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		installedAt := now
		if existing, ok := lock.Skills[candidate.Name].String("installedAt"); ok && existing != "" {
			installedAt = existing
		}
		folderHash := hash
		if wellKnown {
			folderHash = ""
		} else if sourceType == "github" && treeHash != "" {
			folderHash = treeHash
		}
		fields := map[string]any{
			"source": sourceName, "sourceType": sourceType,
			"sourceUrl": sourceURL(source), "skillFolderHash": folderHash,
			"installedAt": installedAt, "updatedAt": now,
		}
		if wellKnown {
			fields["sourceBaseUrl"] = source.URL
			fields["wellKnownDigest"] = wellKnownDigest
		}
		if source.Ref != "" {
			fields["ref"] = source.Ref
		}
		if !wellKnown {
			if candidate.Path != "." {
				fields["skillPath"] = path.Join(candidate.Path, "SKILL.md")
			} else {
				fields["skillPath"] = "SKILL.md"
			}
		}
		if candidate.PluginName != "" {
			fields["pluginName"] = candidate.PluginName
		}
		if err := lock.SetSkill(candidate.Name, fields); err != nil {
			return err
		}
		return lockfile.WriteGlobal(lock)
	}
	lock, err := lockfile.ReadProject(cwd)
	if err != nil {
		return err
	}
	fields := map[string]any{
		"source": sourceName, "sourceType": sourceType, "computedHash": hash,
	}
	if source.Ref != "" {
		fields["ref"] = source.Ref
	}
	if sourceType != "local" {
		fields["sourceUrl"] = source.URL
		if wellKnown {
			fields["sourceBaseUrl"] = source.URL
			fields["wellKnownDigest"] = wellKnownDigest
		} else {
			fields["skillPath"] = path.Join(candidate.Path, "SKILL.md")
		}
	}
	recordSubagents := len(subagents) > 1
	for _, subagent := range subagents {
		if subagent != "" {
			recordSubagents = true
		}
	}
	if recordSubagents {
		fields["subagents"] = subagents
	}
	if candidate.PluginName != "" {
		fields["pluginName"] = candidate.PluginName
	}
	if err := lock.SetSkill(candidate.Name, fields); err != nil {
		return err
	}
	return lockfile.WriteProject(cwd, lock)
}

func lockSource(source skillscan.Source) string {
	if source.Kind == skillscan.LocalSource {
		return source.LocalPath
	}
	if strings.HasPrefix(source.URL, "git@") || strings.HasPrefix(source.URL, "ssh://") {
		return source.URL
	}
	if strings.Contains(strings.ToLower(source.URL), "github.com") {
		if parsed, err := sourceRepoURL(source.URL); err == nil && parsed != "" {
			return parsed
		}
	}
	return source.URL
}

func lockSourceType(source skillscan.Source) string {
	if source.Kind == skillscan.LocalSource {
		return "local"
	}
	lower := strings.ToLower(source.URL)
	if strings.Contains(lower, "github.com") {
		return "github"
	}
	if strings.Contains(lower, "gitlab.com") {
		return "gitlab"
	}
	return "git"
}

func sourceURL(source skillscan.Source) string {
	if source.URL != "" {
		return source.URL
	}
	return source.LocalPath
}

func wellKnownSourceIdentifier(source string) string {
	parsed, err := url.Parse(source)
	if err == nil && parsed.Hostname() != "" {
		return "wellknown/" + parsed.Hostname()
	}
	return "wellknown/unknown"
}

func computeWellKnownDigest(skillDirectory string) (string, error) {
	var files []string
	err := filepath.WalkDir(skillDirectory, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type().IsRegular() {
			relative, err := filepath.Rel(skillDirectory, filename)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, relative := range files {
		contents, err := os.ReadFile(filepath.Join(skillDirectory, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(relative))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(contents)
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func sourceRepoURL(source string) (string, error) {
	trimmed := strings.TrimSuffix(source, ".git")
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimPrefix(trimmed, prefix), nil
		}
	}
	return "", fmt.Errorf("not a GitHub source")
}

func jsonSource(source skillscan.Source) string {
	if source.Kind == skillscan.LocalSource {
		return source.LocalPath
	}
	if strings.HasPrefix(source.URL, "git@") {
		if _, repository, ok := strings.Cut(source.URL, ":"); ok {
			return strings.TrimSuffix(repository, ".git")
		}
	}
	parsed, err := url.Parse(source.URL)
	if err == nil && parsed.Host != "" {
		repository := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
		if strings.Contains(repository, "/") {
			return repository
		}
	}
	return source.URL
}

func scopeName(global bool) string {
	if global {
		return "global"
	}
	return "project"
}

func expandInstallTargets(agents []agent.ID, subagents []string) []addInstallTarget {
	var result []addInstallTarget
	for _, id := range agents {
		if id == "eve" {
			selected := subagents
			if len(selected) == 0 {
				selected = []string{""}
			}
			seen := make(map[string]bool)
			for _, subagent := range selected {
				if subagent == "root" || subagent == "." {
					subagent = ""
				}
				if seen[subagent] {
					continue
				}
				seen[subagent] = true
				label := "Eve"
				if subagent != "" {
					label += " (" + subagent + ")"
				}
				result = append(result, addInstallTarget{Agent: id, Subagent: subagent, Label: label})
			}
			continue
		}
		config, ok := agent.ConfigFor(id)
		if ok {
			result = append(result, addInstallTarget{Agent: id, Label: config.DisplayName})
		}
	}
	return result
}

func eveSubagentValues(targets []addInstallTarget) []string {
	var result []string
	for _, target := range targets {
		if target.Agent == "eve" {
			result = append(result, target.Subagent)
		}
	}
	return result
}

func printAvailableSkills(output io.Writer, candidates []skillscan.Candidate) {
	groups := make(map[string][]skillscan.Candidate)
	for _, candidate := range candidates {
		groups[candidate.PluginName] = append(groups[candidate.PluginName], candidate)
	}
	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		if name != "" {
			groupNames = append(groupNames, name)
		}
	}
	sort.Strings(groupNames)
	for _, name := range groupNames {
		fmt.Fprintf(output, "%s\n", titleCase(name))
		for _, candidate := range groups[name] {
			fmt.Fprintf(output, "  %s — %s\n", candidate.Name, candidate.Description)
		}
	}
	for _, candidate := range groups[""] {
		fmt.Fprintf(output, "%s — %s\n", candidate.Name, candidate.Description)
	}
}

func titleCase(value string) string {
	words := strings.Split(strings.ReplaceAll(value, "_", "-"), "-")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

func registryNames(registry []agent.Config) []string {
	result := make([]string, 0, len(registry))
	for _, config := range registry {
		result = append(result, string(config.ID))
	}
	return result
}

func hasSpecificSkillSelector(values []string) bool {
	return len(values) > 0 && !containsString(values, "*")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func containsCandidate(values []skillscan.Candidate, candidatePath string) bool {
	for _, candidate := range values {
		if candidate.Path == candidatePath {
			return true
		}
	}
	return false
}

func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func writeJSON(output io.Writer, value any) {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func promptEveSubagents(cwd string, input io.Reader, output io.Writer) ([]string, error) {
	choices := append([]string{"root"}, agent.EveSubagents(cwd)...)
	if len(choices) == 1 {
		return []string{"root"}, nil
	}
	fmt.Fprintln(output, "Where should Eve skills be installed?")
	for index, choice := range choices {
		label := choice
		if choice == "root" {
			label = "Root agent"
		}
		fmt.Fprintf(output, "  %d) %s\n", index+1, label)
	}
	fmt.Fprint(output, "Subagent numbers (comma-separated, default root): ")
	line, err := readPromptLine(input)
	if err != nil {
		return nil, fmt.Errorf("read Eve subagent selection: %w", err)
	}
	if line == "" {
		return []string{"root"}, nil
	}
	var selected []string
	seen := make(map[string]bool)
	for _, value := range strings.Split(line, ",") {
		value = strings.TrimSpace(value)
		index := 0
		if _, err := fmt.Sscanf(value, "%d", &index); err == nil && index >= 1 && index <= len(choices) {
			value = choices[index-1]
		} else if value == "." {
			value = "root"
		} else if !containsString(choices, value) {
			return nil, fmt.Errorf("unknown Eve subagent %q", value)
		}
		if !seen[value] {
			seen[value] = true
			selected = append(selected, value)
		}
	}
	return selected, nil
}

func containsAgent(agents []agent.ID, target agent.ID) bool {
	for _, id := range agents {
		if id == target {
			return true
		}
	}
	return false
}

func promptInstallScope(targets []agent.ID, input io.Reader, output io.Writer) (bool, error) {
	supportsGlobal := false
	for _, id := range targets {
		config, ok := agent.ConfigFor(id)
		if ok && config.SupportsGlobal {
			supportsGlobal = true
			break
		}
	}
	if !supportsGlobal {
		return false, nil
	}
	fmt.Fprint(output, "Installation scope (project/global, default project): ")
	choice, err := readPromptLine(input)
	if err != nil {
		return false, fmt.Errorf("read installation scope: %w", err)
	}
	switch strings.ToLower(choice) {
	case "", "p", "project":
		return false, nil
	case "g", "global":
		return true, nil
	default:
		return false, fmt.Errorf("invalid installation scope %q; choose project or global", choice)
	}
}

func selectInstallMode(targets []agent.ID, options addOptions) installer.Mode {
	if options.copy {
		return installer.Copy
	}
	allEve := len(targets) > 0
	uniqueDirectories := make(map[string]bool)
	for _, id := range targets {
		if id != "eve" {
			allEve = false
		}
		config, ok := agent.ConfigFor(id)
		if !ok {
			continue
		}
		directory := config.SkillsDir
		if options.global {
			directory = config.GlobalSkillsDir
		}
		uniqueDirectories[directory] = true
	}
	if allEve || len(uniqueDirectories) <= 1 {
		return installer.Copy
	}
	return installer.Symlink
}

func promptInstallMode(targets []agent.ID, global bool, initial installer.Mode, input io.Reader, output io.Writer) (installer.Mode, error) {
	if initial == installer.Copy {
		return initial, nil
	}
	allEve := len(targets) > 0
	uniqueDirectories := make(map[string]bool)
	for _, id := range targets {
		if id != "eve" {
			allEve = false
		}
		config, ok := agent.ConfigFor(id)
		if !ok {
			continue
		}
		directory := config.SkillsDir
		if global {
			directory = config.GlobalSkillsDir
		}
		uniqueDirectories[directory] = true
	}
	if allEve || len(uniqueDirectories) <= 1 {
		return installer.Copy, nil
	}
	fmt.Fprint(output, "Installation method (symlink/copy, default symlink): ")
	choice, err := readPromptLine(input)
	if err != nil {
		return "", fmt.Errorf("read installation method: %w", err)
	}
	switch strings.ToLower(choice) {
	case "", "s", "symlink":
		return installer.Symlink, nil
	case "c", "copy":
		return installer.Copy, nil
	default:
		return "", fmt.Errorf("invalid installation method %q; choose symlink or copy", choice)
	}
}

func promptInstallConfirmation(input io.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Proceed with installation? [Y/n] ")
	choice, err := readPromptLine(input)
	if err != nil {
		return false, fmt.Errorf("read installation confirmation: %w", err)
	}
	switch strings.ToLower(choice) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid confirmation %q; enter yes or no", choice)
	}
}

func readPromptLine(input io.Reader) (string, error) {
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func addFailure(stdout, stderr io.Writer, jsonMode bool, message string) int {
	fmt.Fprintf(stderr, "skills-go: %s\n", message)
	if jsonMode {
		writeJSON(stdout, []addJSONResult{{Status: "failed", Error: message, Security: nil}})
	}
	return 1
}
