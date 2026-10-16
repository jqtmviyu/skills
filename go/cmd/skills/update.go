package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type updateOptions struct {
	global  bool
	project bool
	yes     bool
	skills  []string
}

type updateItem struct {
	name  string
	entry lockfile.Entry
}

type updateLocation struct {
	item      updateItem
	candidate skillscan.Candidate
	path      string
	moved     bool
}

type githubTreeDocument struct {
	SHA       string            `json:"sha"`
	Truncated bool              `json:"truncated"`
	Tree      []githubTreeEntry `json:"tree"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type githubRepositoryMetadata struct {
	DefaultBranch string `json:"default_branch"`
}

func parseUpdateArgs(args []string) (updateOptions, error) {
	var options updateOptions
	for _, arg := range args {
		switch arg {
		case "-g", "--global":
			options.global = true
		case "-p", "--project":
			options.project = true
		case "-y", "--yes":
			options.yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown update option: %s", arg)
			}
			options.skills = append(options.skills, arg)
		}
	}
	return options, nil
}

func runUpdate(args []string, stdout, stderr io.Writer) int {
	options, err := parseUpdateArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "skills: %v\n", err)
		return 1
	}
	cwd, err := currentDirectory()
	if err != nil {
		fmt.Fprintf(stderr, "skills: %v\n", err)
		return 1
	}
	global, project, err := resolveUpdateScope(options, cwd, os.Stdin, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "skills: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Checking for skill updates...")
	failed := false
	found := 0
	if global {
		count, checkFailed := updateGlobalSkills(options, stdout, stderr)
		found += count
		failed = failed || checkFailed
	}
	if project {
		count, checkFailed := updateProjectSkills(options, cwd, stdout, stderr)
		found += count
		failed = failed || checkFailed
	}
	if len(options.skills) > 0 && found == 0 {
		fmt.Fprintf(stdout, "No installed skills found matching: %s\n", strings.Join(options.skills, ", "))
	}
	if failed {
		return 1
	}
	return 0
}

func resolveUpdateScope(options updateOptions, cwd string, input io.Reader, output io.Writer) (bool, bool, error) {
	if options.global && options.project {
		return true, true, nil
	}
	if options.global {
		return true, false, nil
	}
	if options.project {
		return false, true, nil
	}
	if len(options.skills) > 0 {
		return true, true, nil
	}
	if options.yes || !isTerminal(input) {
		hasProject := hasProjectSkills(cwd)
		return !hasProject, hasProject, nil
	}
	fmt.Fprint(output, "Update scope (project/global/both, default project): ")
	choice, err := readPromptLine(input)
	if err != nil {
		return false, false, fmt.Errorf("read update scope: %w", err)
	}
	switch strings.ToLower(choice) {
	case "", "p", "project":
		return false, true, nil
	case "g", "global":
		return true, false, nil
	case "b", "both":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("invalid update scope %q; choose project, global, or both", choice)
	}
}

func hasProjectSkills(cwd string) bool {
	if _, err := os.Stat(filepath.Join(cwd, "skills-lock.json")); err == nil {
		return true
	}
	canonical := filepath.Join(cwd, ".agents", "skills")
	entries, err := os.ReadDir(canonical)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(canonical, entry.Name(), "SKILL.md")); err == nil {
				return true
			}
		}
	}
	return false
}

func matchesUpdateFilter(name string, entry lockfile.Entry, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	source, _ := entry.String("source")
	sourceURL, _ := entry.String("sourceUrl")
	sourceBaseURL, _ := entry.String("sourceBaseUrl")
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		for _, candidate := range []string{name, source, sourceURL, sourceBaseURL} {
			if strings.EqualFold(strings.TrimSpace(candidate), filter) {
				return true
			}
		}
	}
	return false
}

func updateGlobalSkills(options updateOptions, stdout, stderr io.Writer) (int, bool) {
	lock, err := lockfile.ReadGlobal()
	if err != nil {
		fmt.Fprintf(stderr, "skills: read global lock: %v\n", err)
		return 0, true
	}
	groups := make(map[string][]updateItem)
	wellKnownGroups := make(map[string][]updateItem)
	var keys, wellKnownKeys []string
	found := 0
	for name, entry := range lock.Skills {
		if !matchesUpdateFilter(name, entry, options.skills) {
			continue
		}
		found++
		sourceType, _ := entry.String("sourceType")
		if sourceType == "well-known" {
			baseURL, hasBaseURL := entry.String("sourceBaseUrl")
			digest, hasDigest := entry.String("wellKnownDigest")
			if !hasBaseURL || baseURL == "" || !hasDigest || digest == "" {
				fmt.Fprintf(stdout, "Skipping global skill %s: no supported well-known version metadata.\n", name)
				continue
			}
			if _, exists := wellKnownGroups[baseURL]; !exists {
				wellKnownKeys = append(wellKnownKeys, baseURL)
			}
			wellKnownGroups[baseURL] = append(wellKnownGroups[baseURL], updateItem{name: name, entry: entry})
			continue
		}
		hash, hasHash := entry.String("skillFolderHash")
		skillPath, hasPath := entry.String("skillPath")
		if sourceType == "local" || !hasHash || hash == "" || !hasPath || skillPath == "" {
			fmt.Fprintf(stdout, "Skipping global skill %s: no supported version metadata.\n", name)
			continue
		}
		key := globalSourceKey(entry)
		if _, exists := groups[key]; !exists {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], updateItem{name: name, entry: entry})
	}
	sort.Strings(keys)
	sort.Strings(wellKnownKeys)
	failed := false
	wellKnownLockChanged := false
	for _, baseURL := range wellKnownKeys {
		items := wellKnownGroups[baseURL]
		changed, checkErr := changedWellKnownItems(baseURL, items)
		if checkErr != nil {
			fmt.Fprintf(stdout, "Could not check well-known skills from %s: %v\n", baseURL, checkErr)
			continue
		}
		if len(changed) == 0 {
			continue
		}
		fmt.Fprintf(stdout, "Found %d global update(s) from %s.\n", len(changed), baseURL)
		lockChanged, updateFailed := updateWellKnownItems(baseURL, changed, true, "", lock, nil, stdout, stderr)
		wellKnownLockChanged = wellKnownLockChanged || lockChanged
		failed = failed || updateFailed
	}
	if wellKnownLockChanged {
		if err := lockfile.WriteGlobal(lock); err != nil {
			fmt.Fprintf(stderr, "skills: write Global well-known lock: %v\n", err)
			failed = true
		}
	}
	for _, key := range keys {
		items := groups[key]
		first := items[0].entry
		sourceType, _ := first.String("sourceType")
		tree, apiErr := fetchGitHubTree(first)
		var changed []updateLocation
		if sourceType == "github" && apiErr == nil && tree != nil {
			missingPath := false
			for _, item := range items {
				skillPath, _ := item.entry.String("skillPath")
				if !treeHasBlob(tree, skillPath) {
					missingPath = true
					break
				}
			}
			if !missingPath {
				for _, item := range items {
					lockedPath, _ := item.entry.String("skillPath")
					lockedHash, _ := item.entry.String("skillFolderHash")
					if latest, ok := lockfile.SkillFolderTreeHash(tree.SHA, convertTreeEntries(tree.Tree), lockedPath); ok && latest != lockedHash {
						changed = append(changed, updateLocation{item: item, path: pathForSkillFile(lockedPath)})
					}
				}
				if len(changed) > 0 {
					fmt.Fprintf(stdout, "Found %d global update(s) from %s.\n", len(changed), key)
					changed = updateGlobalFromScan(changed, stdout, stderr, &failed)
				}
				continue
			}
			fmt.Fprintf(stderr, "skills: locked skill path changed for %s; checking with partial Git scan\n", key)
		} else if sourceType == "github" {
			fmt.Fprintf(stderr, "skills: GitHub Trees API unavailable for %s; checking with partial Git scan\n", key)
		}
		changed = checkGroupWithPartialGit(first, items, true, stdout, stderr, &failed)
		if len(changed) > 0 {
			fmt.Fprintf(stdout, "Found %d global update(s) from %s.\n", len(changed), key)
			updateGlobalFromScan(changed, stdout, stderr, &failed)
		}
	}
	return found, failed
}

func globalSourceKey(entry lockfile.Entry) string {
	source, _ := entry.String("source")
	ref, _ := entry.String("ref")
	return source + "\n" + ref
}

func changedWellKnownItems(baseURL string, items []updateItem) ([]updateItem, error) {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.name)
	}
	digests, err := fetchWellKnownDigests(baseURL, names)
	if err != nil {
		return nil, err
	}
	var changed []updateItem
	for _, item := range items {
		current, exists := digests[item.name]
		locked, _ := item.entry.String("wellKnownDigest")
		if exists && current != locked {
			changed = append(changed, item)
		}
	}
	return changed, nil
}

func updateWellKnownItems(baseURL string, items []updateItem, global bool, cwd string, globalLock *lockfile.GlobalLock, projectLock *lockfile.ProjectLock, stdout, stderr io.Writer) (bool, bool) {
	session, err := openAddSource(context.Background(), baseURL, skillscan.Options{FullDepth: true, IncludeInternal: true, IncludeDuplicateNames: true})
	if err != nil {
		fmt.Fprintf(stderr, "skills: fetch well-known updates from %s: %v\n", baseURL, err)
		return false, true
	}
	defer session.Close()
	if !session.wellKnown {
		fmt.Fprintf(stderr, "skills: source %s no longer provides a supported well-known index\n", baseURL)
		return false, true
	}

	candidates := make(map[string][]skillscan.Candidate)
	for _, candidate := range session.scan.Candidates() {
		key := strings.ToLower(candidate.Name)
		candidates[key] = append(candidates[key], candidate)
	}
	selected := make(map[string]skillscan.Candidate, len(items))
	var paths []string
	failed := false
	for _, item := range items {
		matches := candidates[strings.ToLower(item.name)]
		if len(matches) != 1 {
			fmt.Fprintf(stderr, "skills: well-known skill %s is missing or ambiguous at %s\n", item.name, baseURL)
			failed = true
			continue
		}
		selected[item.name] = matches[0]
		paths = append(paths, matches[0].Path)
	}
	if len(paths) == 0 {
		return false, failed
	}
	materialized, err := session.scan.Materialize(paths)
	if err != nil {
		fmt.Fprintf(stderr, "skills: materialize well-known updates from %s: %v\n", baseURL, err)
		return false, true
	}
	byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
	for _, skill := range materialized {
		byPath[skill.Candidate.Path] = skill
	}
	changedLock := false
	for _, item := range items {
		candidate, ok := selected[item.name]
		if !ok {
			continue
		}
		skill, ok := byPath[candidate.Path]
		if !ok {
			fmt.Fprintf(stderr, "skills: materialized well-known skill %s is missing\n", item.name)
			failed = true
			continue
		}
		if err := installManagedSkill(item.name, skill.Directory, global, cwd, lockStringArray(item.entry, "subagents")); err != nil {
			fmt.Fprintf(stderr, "skills: update well-known skill %s: %v\n", item.name, err)
			failed = true
			continue
		}
		digest := session.wellKnownDigests[candidate.Name]
		if digest == "" {
			digest, err = computeWellKnownDigest(skill.Directory)
			if err != nil {
				fmt.Fprintf(stderr, "skills: hash updated well-known skill %s: %v\n", item.name, err)
				failed = true
				continue
			}
		}
		if global {
			if globalLock == nil {
				fmt.Fprintf(stderr, "skills: Global lock is unavailable for well-known skill %s\n", item.name)
				failed = true
				continue
			}
			entry := globalLock.Skills[item.name]
			if err := entry.Set("wellKnownDigest", digest); err != nil {
				fmt.Fprintf(stderr, "skills: update Global lock entry %s: %v\n", item.name, err)
				failed = true
				continue
			}
			_ = entry.Set("updatedAt", time.Now().UTC().Format(time.RFC3339Nano))
			globalLock.Skills[item.name] = entry
		} else {
			if projectLock == nil {
				fmt.Fprintf(stderr, "skills: Project lock is unavailable for well-known skill %s\n", item.name)
				failed = true
				continue
			}
			projectHash, hashErr := lockfile.ComputeProjectHash(skill.Directory)
			if hashErr != nil {
				fmt.Fprintf(stderr, "skills: hash updated Project skill %s: %v\n", item.name, hashErr)
				failed = true
				continue
			}
			entry := projectLock.Skills[item.name]
			if err := entry.Set("wellKnownDigest", digest); err != nil {
				fmt.Fprintf(stderr, "skills: update Project lock digest %s: %v\n", item.name, err)
				failed = true
				continue
			}
			if err := entry.Set("computedHash", projectHash); err != nil {
				fmt.Fprintf(stderr, "skills: update Project lock hash %s: %v\n", item.name, err)
				failed = true
				continue
			}
			projectLock.Skills[item.name] = entry
		}
		changedLock = true
		fmt.Fprintf(stdout, "Updated %s well-known skill %s.\n", scopeName(global), item.name)
	}
	return changedLock, failed
}

func fetchGitHubTree(entry lockfile.Entry) (*githubTreeDocument, error) {
	owner, repository, ok := githubRepository(entry)
	if !ok {
		return nil, fmt.Errorf("source is not hosted on github.com")
	}
	base := strings.TrimRight(os.Getenv("SKILLS_GO_TEST_GITHUB_API_URL"), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	ref, _ := entry.String("ref")
	refs := []string{ref}
	client := &http.Client{Timeout: 15 * time.Second}
	if ref == "" {
		endpoint := base + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository)
		body, err := fetchGitHubAPIResponse(client, endpoint)
		if err != nil {
			return nil, fmt.Errorf("resolve repository default branch: %w", err)
		}
		var metadata githubRepositoryMetadata
		if err := json.Unmarshal(body, &metadata); err != nil {
			return nil, fmt.Errorf("decode repository metadata: %w", err)
		}
		if metadata.DefaultBranch == "" {
			return nil, fmt.Errorf("repository metadata has no default branch")
		}
		refs = []string{metadata.DefaultBranch}
	}
	var lastErr error
	for _, currentRef := range refs {
		endpoint := base + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/git/trees/" + url.PathEscape(currentRef) + "?recursive=1"
		body, err := fetchGitHubAPIResponse(client, endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		var tree githubTreeDocument
		if err := json.Unmarshal(body, &tree); err != nil {
			lastErr = err
			continue
		}
		if tree.Truncated || tree.SHA == "" {
			lastErr = fmt.Errorf("Trees API response is truncated or incomplete")
			continue
		}
		return &tree, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Trees API returned no tree")
	}
	return nil, lastErr
}

func fetchGitHubAPIResponse(client *http.Client, endpoint string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", response.StatusCode)
	}
	return body, nil
}

func githubRepository(entry lockfile.Entry) (string, string, bool) {
	sourceURL, hasURL := entry.String("sourceUrl")
	if hasURL && sourceURL != "" {
		parsed, err := url.Parse(sourceURL)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
			return "", "", false
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) != 2 {
			return "", "", false
		}
		return parts[0], strings.TrimSuffix(parts[1], ".git"), true
	}
	source, ok := entry.String("source")
	if !ok || !isGitHubShorthand(source) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimSuffix(source, ".git"), "/")
	return parts[0], parts[1], true
}

func treeHasBlob(tree *githubTreeDocument, skillPath string) bool {
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && entry.Path == strings.ReplaceAll(skillPath, "\\", "/") {
			return true
		}
	}
	return false
}

func convertTreeEntries(entries []githubTreeEntry) []lockfile.TreeEntry {
	result := make([]lockfile.TreeEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, lockfile.TreeEntry{Path: entry.Path, Type: entry.Type, SHA: entry.SHA})
	}
	return result
}

func updateProjectSkills(options updateOptions, cwd string, stdout, stderr io.Writer) (int, bool) {
	lock, err := lockfile.ReadProject(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "skills: read project lock: %v\n", err)
		return 0, true
	}
	groups := make(map[string][]updateItem)
	wellKnownGroups := make(map[string][]updateItem)
	var keys, wellKnownKeys []string
	found := 0
	failed := false
	for name, entry := range lock.Skills {
		if !matchesUpdateFilter(name, entry, options.skills) {
			continue
		}
		found++
		sourceType, _ := entry.String("sourceType")
		if sourceType == "well-known" {
			baseURL, hasBaseURL := entry.String("sourceBaseUrl")
			if !hasBaseURL || baseURL == "" {
				baseURL, hasBaseURL = entry.String("sourceUrl")
			}
			digest, hasDigest := entry.String("wellKnownDigest")
			if !hasBaseURL || baseURL == "" || !hasDigest || digest == "" {
				fmt.Fprintf(stdout, "Skipping project skill %s: no supported well-known version metadata.\n", name)
				continue
			}
			if _, exists := wellKnownGroups[baseURL]; !exists {
				wellKnownKeys = append(wellKnownKeys, baseURL)
			}
			wellKnownGroups[baseURL] = append(wellKnownGroups[baseURL], updateItem{name: name, entry: entry})
			continue
		}
		if sourceType == "local" || sourceType == "node_modules" {
			continue
		}
		if skillPath, ok := entry.String("skillPath"); !ok || skillPath == "" {
			fmt.Fprintf(stdout, "Skipping project skill %s: installed before skillPath tracking.\n", name)
			continue
		}
		input, sourceErr := restoreScanInput(entry)
		if sourceErr != nil {
			fmt.Fprintf(stderr, "skills: cannot update project skill %s: %v\n", name, sourceErr)
			failed = true
			continue
		}
		if _, exists := groups[input]; !exists {
			keys = append(keys, input)
		}
		groups[input] = append(groups[input], updateItem{name: name, entry: entry})
	}
	sort.Strings(keys)
	sort.Strings(wellKnownKeys)
	changedAny := false
	for _, baseURL := range wellKnownKeys {
		items := wellKnownGroups[baseURL]
		changed, checkErr := changedWellKnownItems(baseURL, items)
		if checkErr != nil {
			fmt.Fprintf(stdout, "Could not check well-known skills from %s: %v\n", baseURL, checkErr)
			continue
		}
		if len(changed) == 0 {
			continue
		}
		fmt.Fprintf(stdout, "Found %d project update(s) from %s.\n", len(changed), baseURL)
		lockChanged, updateFailed := updateWellKnownItems(baseURL, changed, false, cwd, nil, lock, stdout, stderr)
		changedAny = changedAny || lockChanged
		failed = failed || updateFailed
	}
	for _, input := range keys {
		items := groups[input]
		changed := checkGroupWithPartialGit(items[0].entry, items, false, stdout, stderr, &failed)
		if len(changed) == 0 {
			continue
		}
		session, openErr := openAddSource(context.Background(), input, skillscan.Options{FullDepth: true, IncludeInternal: true, IncludeDuplicateNames: true})
		if openErr != nil {
			fmt.Fprintf(stderr, "skills: cannot install updates from %s: %v\n", input, openErr)
			failed = true
			continue
		}
		paths := make([]string, 0, len(changed))
		for _, update := range changed {
			paths = append(paths, update.candidate.Path)
		}
		materialized, materializeErr := session.scan.Materialize(paths)
		if materializeErr != nil {
			fmt.Fprintf(stderr, "skills: materialize updates from %s: %v\n", input, materializeErr)
			failed = true
			session.Close()
			continue
		}
		byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
		for _, skill := range materialized {
			byPath[skill.Candidate.Path] = skill
		}
		for _, update := range changed {
			skill := byPath[update.candidate.Path]
			newHash, hashErr := lockfile.ComputeProjectHash(skill.Directory)
			if hashErr != nil {
				fmt.Fprintf(stderr, "skills: hash updated skill %s: %v\n", update.item.name, hashErr)
				failed = true
				continue
			}
			if err := installManagedSkill(update.item.name, skill.Directory, false, cwd, lockStringArray(update.item.entry, "subagents")); err != nil {
				fmt.Fprintf(stderr, "skills: update %s: %v\n", update.item.name, err)
				failed = true
				continue
			}
			fields := map[string]any{"computedHash": newHash}
			if update.candidate.Path != "" {
				fields["skillPath"] = path.Join(update.candidate.Path, "SKILL.md")
			}
			if err := lock.Skills[update.item.name].Set("computedHash", newHash); err != nil {
				fmt.Fprintf(stderr, "skills: update lock entry %s: %v\n", update.item.name, err)
				failed = true
				continue
			}
			if update.candidate.Path != "" {
				_ = lock.Skills[update.item.name].Set("skillPath", fields["skillPath"])
			}
			changedAny = true
			fmt.Fprintf(stdout, "Updated project skill %s.\n", update.item.name)
		}
		session.Close()
	}
	if changedAny {
		if err := lockfile.WriteProject(cwd, lock); err != nil {
			fmt.Fprintf(stderr, "skills: write project lock: %v\n", err)
			failed = true
		}
	}
	return found, failed
}

func checkGroupWithPartialGit(first lockfile.Entry, items []updateItem, global bool, stdout, stderr io.Writer, failed *bool) []updateLocation {
	input, err := restoreScanInput(first)
	if err != nil {
		fmt.Fprintf(stderr, "skills: cannot check source: %v\n", err)
		*failed = true
		return nil
	}
	session, err := openAddSource(context.Background(), input, skillscan.Options{FullDepth: true, IncludeInternal: true, IncludeDuplicateNames: true})
	if err != nil {
		fmt.Fprintf(stderr, "skills: cannot check source %s: %v\n", input, err)
		*failed = true
		return nil
	}
	defer session.Close()
	if status := session.scan.FilterStatus(); status.Message != "" {
		fmt.Fprintf(stderr, "Warning: %s\n", status.Message)
	}
	locations, deleted, ambiguous := resolveUpdateLocations(items, session.scan.Candidates())
	for _, name := range deleted {
		fmt.Fprintf(stdout, "Skill %s appears deleted upstream; keeping the local installation and lock.\n", name)
	}
	for _, name := range ambiguous {
		fmt.Fprintf(stderr, "skills: multiple current paths match %s; leaving it unchanged\n", name)
	}
	if global {
		var changed []updateLocation
		for _, location := range locations {
			locked, _ := location.item.entry.String("skillFolderHash")
			latest := ""
			if isGitSHA1(locked) {
				latest, _ = session.scan.GitTreeHash(location.candidate.Path)
			} else {
				materialized, materializeErr := session.scan.Materialize([]string{location.candidate.Path})
				if materializeErr == nil && len(materialized) == 1 {
					latest, _ = lockfile.ComputeProjectHash(materialized[0].Directory)
				}
			}
			if latest == "" {
				fmt.Fprintf(stderr, "skills: cannot compute update hash for %s\n", location.item.name)
				*failed = true
				continue
			}
			if location.moved || latest != locked {
				changed = append(changed, location)
			}
		}
		return changed
	}

	paths := make([]string, 0, len(locations))
	for _, location := range locations {
		paths = append(paths, location.candidate.Path)
	}
	materialized, err := session.scan.Materialize(paths)
	if err != nil {
		fmt.Fprintf(stderr, "skills: materialize locked skill directories: %v\n", err)
		*failed = true
		return nil
	}
	byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
	for _, skill := range materialized {
		byPath[skill.Candidate.Path] = skill
	}
	var changed []updateLocation
	for _, location := range locations {
		skill := byPath[location.candidate.Path]
		latest, hashErr := lockfile.ComputeProjectHash(skill.Directory)
		if hashErr != nil {
			fmt.Fprintf(stderr, "skills: hash project skill %s: %v\n", location.item.name, hashErr)
			*failed = true
			continue
		}
		locked, _ := location.item.entry.String("computedHash")
		if location.moved || latest != locked {
			location.path = skill.Directory
			changed = append(changed, location)
		}
	}
	return changed
}

func resolveUpdateLocations(items []updateItem, candidates []skillscan.Candidate) ([]updateLocation, []string, []string) {
	byPath := make(map[string]skillscan.Candidate)
	byName := make(map[string][]skillscan.Candidate)
	for _, candidate := range candidates {
		byPath[path.Clean(candidate.Path)] = candidate
		key := normalizeLockedSkillName(candidate.Name)
		byName[key] = append(byName[key], candidate)
	}
	var locations []updateLocation
	var deleted, ambiguous []string
	for _, item := range items {
		lockedPath, hasPath := item.entry.String("skillPath")
		if !hasPath || lockedPath == "" {
			continue
		}
		folder := pathForSkillFile(lockedPath)
		candidatesByName := byName[normalizeLockedSkillName(item.name)]
		pathAddressable := canTargetSkillPath(item.entry)
		exact, exactExists := byPath[path.Clean(folder)]
		if exactExists && pathAddressable {
			locations = append(locations, updateLocation{item: item, candidate: exact, path: exact.Path, moved: exact.Path != folder})
			continue
		}
		if len(candidatesByName) > 1 {
			ambiguous = append(ambiguous, item.name)
			continue
		}
		if exactExists {
			locations = append(locations, updateLocation{item: item, candidate: exact, path: exact.Path, moved: exact.Path != folder})
			continue
		}
		if len(candidatesByName) == 1 {
			candidate := candidatesByName[0]
			locations = append(locations, updateLocation{item: item, candidate: candidate, path: candidate.Path, moved: candidate.Path != folder})
			continue
		}
		deleted = append(deleted, item.name)
	}
	return locations, deleted, ambiguous
}

func isGitSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func normalizeLockedSkillName(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "-", " ", "-").Replace(name))
}

func pathForSkillFile(skillPath string) string {
	cleaned := path.Clean(strings.ReplaceAll(skillPath, "\\", "/"))
	if strings.HasSuffix(strings.ToLower(cleaned), "/skill.md") {
		return strings.TrimSuffix(cleaned, "/SKILL.md")
	}
	if strings.EqualFold(cleaned, "SKILL.md") {
		return "."
	}
	return path.Dir(cleaned)
}

func canTargetSkillPath(entry lockfile.Entry) bool {
	sourceType, _ := entry.String("sourceType")
	source, _ := entry.String("source")
	sourceURL, _ := entry.String("sourceUrl")
	if sourceType == "github" && isGitHubShorthand(source) {
		return true
	}
	if sourceURL == "" {
		sourceURL = source
	}
	if strings.HasPrefix(sourceURL, "git@") || strings.HasPrefix(sourceURL, "ssh://") || strings.HasSuffix(sourceURL, ".git") {
		return false
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "github.com" || host == "gitlab.com"
}

func updateGlobalFromScan(changed []updateLocation, stdout, stderr io.Writer, failed *bool) []updateLocation {
	if len(changed) == 0 {
		return nil
	}
	input, err := restoreScanInput(changed[0].item.entry)
	if err != nil {
		fmt.Fprintf(stderr, "skills: cannot update global source: %v\n", err)
		*failed = true
		return nil
	}
	session, err := openAddSource(context.Background(), input, skillscan.Options{FullDepth: true, IncludeInternal: true, IncludeDuplicateNames: true})
	if err != nil {
		fmt.Fprintf(stderr, "skills: cannot install global updates from %s: %v\n", input, err)
		*failed = true
		return nil
	}
	defer session.Close()
	var selected []updateLocation
	for _, update := range changed {
		var matches []skillscan.Candidate
		for _, candidate := range session.scan.Candidates() {
			if path.Clean(candidate.Path) == path.Clean(update.path) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			update.candidate = matches[0]
			selected = append(selected, update)
		}
	}
	paths := make([]string, 0, len(selected))
	for _, update := range selected {
		paths = append(paths, update.candidate.Path)
	}
	materialized, err := session.scan.Materialize(paths)
	if err != nil {
		fmt.Fprintf(stderr, "skills: materialize global updates: %v\n", err)
		*failed = true
		return nil
	}
	byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
	for _, skill := range materialized {
		byPath[skill.Candidate.Path] = skill
	}
	lock, err := lockfile.ReadGlobal()
	if err != nil {
		fmt.Fprintf(stderr, "skills: read global lock: %v\n", err)
		*failed = true
		return nil
	}
	changedLock := false
	for _, update := range selected {
		skill := byPath[update.candidate.Path]
		if err := installManagedSkill(update.item.name, skill.Directory, true, "", nil); err != nil {
			fmt.Fprintf(stderr, "skills: update global skill %s: %v\n", update.item.name, err)
			*failed = true
			continue
		}
		entry := lock.Skills[update.item.name]
		lockedHash, _ := entry.String("skillFolderHash")
		latest := ""
		if isGitSHA1(lockedHash) {
			latest, _ = session.scan.GitTreeHash(update.candidate.Path)
		} else {
			latest, _ = lockfile.ComputeProjectHash(skill.Directory)
		}
		if latest == "" {
			fmt.Fprintf(stderr, "skills: cannot compute update hash for updated skill %s\n", update.item.name)
			*failed = true
			continue
		}
		if latest != "" {
			_ = entry.Set("skillFolderHash", latest)
		}
		_ = entry.Set("skillPath", path.Join(update.candidate.Path, "SKILL.md"))
		_ = entry.Set("updatedAt", time.Now().UTC().Format(time.RFC3339Nano))
		lock.Skills[update.item.name] = entry
		changedLock = true
		fmt.Fprintf(stdout, "Updated global skill %s.\n", update.item.name)
	}
	if changedLock {
		if err := lockfile.WriteGlobal(lock); err != nil {
			fmt.Fprintf(stderr, "skills: write global lock: %v\n", err)
			*failed = true
		}
	}
	return selected
}

func lockStringArray(entry lockfile.Entry, key string) []string {
	var values []string
	if err := json.Unmarshal(entry[key], &values); err != nil {
		return nil
	}
	return values
}

func installManagedSkill(name, sourceDirectory string, global bool, cwd string, eveSubagents []string) error {
	canonical, err := installer.GetCanonicalPath(name, agent.UniversalID, installer.Options{Global: global, CWD: cwd})
	if err != nil {
		return err
	}
	if hasSkillMarkdown(canonical) {
		_, err = installer.InstallSkill(installer.Skill{Name: name, Path: sourceDirectory}, agent.UniversalID, installer.Options{Global: global, CWD: cwd, Mode: installer.Copy})
		if err != nil {
			return err
		}
	}
	installedCopy := false
	installedEveTargets := make(map[string]bool)
	for _, config := range agent.Registry() {
		if global && !config.SupportsGlobal {
			continue
		}
		target, targetErr := agent.ResolveTarget(config.ID, name, agent.TargetOptions{Global: global, CWD: cwd})
		if targetErr != nil || agent.IsUniversal(config.ID) && sameFilesystemPath(target.AgentDir, target.CanonicalDir) {
			continue
		}
		if !hasSkillMarkdown(target.AgentDir) {
			continue
		}
		info, statErr := os.Lstat(target.AgentDir)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			if !hasSkillMarkdown(canonical) {
				_, err = installer.InstallSkill(installer.Skill{Name: name, Path: sourceDirectory}, agent.UniversalID, installer.Options{Global: global, CWD: cwd, Mode: installer.Copy})
				if err != nil {
					return err
				}
			}
			continue
		}
		_, err = installer.InstallSkill(installer.Skill{Name: name, Path: sourceDirectory}, config.ID, installer.Options{Global: global, CWD: cwd, Mode: installer.Copy})
		if err != nil {
			return err
		}
		installedCopy = true
		if config.ID == "eve" {
			installedEveTargets[""] = true
		}
	}
	for _, subagent := range eveSubagents {
		if subagent == "root" || subagent == "." {
			subagent = ""
		}
		if installedEveTargets[subagent] {
			continue
		}
		_, err = installer.InstallSkill(installer.Skill{Name: name, Path: sourceDirectory}, "eve", installer.Options{CWD: cwd, EveSubagent: subagent, Mode: installer.Copy})
		if err != nil {
			return err
		}
		installedEveTargets[subagent] = true
		installedCopy = true
	}
	if !hasSkillMarkdown(canonical) && !installedCopy {
		_, err = installer.InstallSkill(installer.Skill{Name: name, Path: sourceDirectory}, agent.UniversalID, installer.Options{Global: global, CWD: cwd, Mode: installer.Copy})
	}
	return err
}

func sameFilesystemPath(first, second string) bool {
	firstAbsolute, firstErr := filepath.Abs(first)
	secondAbsolute, secondErr := filepath.Abs(second)
	return firstErr == nil && secondErr == nil && filepath.Clean(firstAbsolute) == filepath.Clean(secondAbsolute)
}
