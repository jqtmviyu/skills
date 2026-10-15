package skillscan

import (
	"encoding/json"
	"os"
	"path"
	"strings"
)

var projectSkillDirectories = []string{
	".agents/skills", ".claude/skills", ".cline/skills", ".codebuddy/skills", ".codex/skills",
	".commandcode/skills", ".continue/skills", ".factory/skills", ".github/skills", ".goose/skills",
	".grok/skills", ".iflow/skills", ".junie/skills", ".kilo/skills", ".kilocode/skills",
	".kimchi/skills", ".kiro/skills", ".minimax/skills", ".mux/skills", ".neovate/skills",
	".opencode/skills", ".openhands/skills", ".pi/skills", ".posit/assistant/skills", ".qoder/skills",
	".roo/skills", ".trae/skills", ".windsurf/skills", ".zcode/skills", ".zencoder/skills",
}

var skippedDirectories = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true, "__pycache__": true,
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []pluginEntry `json:"plugins"`
}

type pluginEntry struct {
	Source any      `json:"source"`
	Skills []string `json:"skills"`
	Name   string   `json:"name"`
}

type singlePluginManifest struct {
	Skills []string `json:"skills"`
	Name   string   `json:"name"`
}

type pluginDiscovery struct {
	searchDirectories []string
	groupings         map[string]string
}

type projectLockFile struct {
	Version float64 `json:"version"`
	Skills  map[string]struct {
		SourceType string `json:"sourceType"`
	} `json:"skills"`
}

func skippedDirectory(name string) bool { return skippedDirectories[name] }

func (scan *Scan) discover() error {
	scan.lockedSkillNames = scan.readLockedSkillNames()
	plugin := scan.readPluginMetadata()
	seenNames := make(map[string]bool)

	rootSkill := scan.repoPath("SKILL.md")
	if scan.hasFile(rootSkill) {
		if candidate, ok := scan.readCandidate(rootSkill, plugin.groupings[scan.repositorySkillDirectory(".")]); ok {
			scan.addCandidate(candidate, seenNames)
			if !scan.options.FullDepth {
				return nil
			}
		}
	}

	searchDirectories := []string{"."}
	searchDirectories = append(searchDirectories, "skills", "skills/.curated", "skills/.experimental", "skills/.system")
	searchDirectories = append(searchDirectories, projectSkillDirectories...)
	deepDirectories := make(map[string]bool)
	for _, directory := range searchDirectories[1:] {
		deepDirectories[directory] = true
	}
	searchDirectories = append(searchDirectories, plugin.searchDirectories...)

	for _, directory := range uniquePaths(searchDirectories) {
		maxDepth := 1
		if deepDirectories[directory] {
			maxDepth = 3
		}
		scan.walkSkillDirectories(directory, maxDepth, 1, seenNames, plugin.groupings)
	}

	if len(scan.candidates) == 0 || scan.options.FullDepth {
		scan.findAllSkillDirectories(".", 0, seenNames, plugin.groupings)
	}
	return nil
}

func (scan *Scan) readLockedSkillNames() map[string]bool {
	lockedNames := make(map[string]bool)
	raw, err := scan.readRepositoryFile("skills-lock.json")
	if err != nil {
		return lockedNames
	}
	var lock projectLockFile
	if json.Unmarshal(raw, &lock) != nil || lock.Version < 1 || lock.Skills == nil {
		return lockedNames
	}
	for name := range lock.Skills {
		lockedNames[normalizeSkillName(name)] = true
	}
	return lockedNames
}

func (scan *Scan) readPluginMetadata() pluginDiscovery {
	result := pluginDiscovery{groupings: make(map[string]string)}
	searchRoot := scan.searchRoot()

	marketplacePath := scan.repoPath(".claude-plugin/marketplace.json")
	if raw, err := scan.readRepositoryFile(marketplacePath); err == nil {
		var manifest marketplaceManifest
		if json.Unmarshal(raw, &manifest) == nil && (manifest.Metadata.PluginRoot == "" || strings.HasPrefix(manifest.Metadata.PluginRoot, "./")) {
			for _, plugin := range manifest.Plugins {
				pluginSource, ok := plugin.Source.(string)
				if plugin.Source != nil && !ok {
					continue
				}
				if !validPluginRelative(pluginSource) {
					continue
				}
				pluginBase := secureJoin(searchRoot, manifest.Metadata.PluginRoot, pluginSource)
				if pluginBase == "" {
					continue
				}
				result.addPlugin(pluginBase, plugin.Skills, plugin.Name, searchRoot)
			}
		}
	}

	pluginPath := scan.repoPath(".claude-plugin/plugin.json")
	if raw, err := scan.readRepositoryFile(pluginPath); err == nil {
		var manifest singlePluginManifest
		if json.Unmarshal(raw, &manifest) == nil {
			result.addPlugin(searchRoot, manifest.Skills, manifest.Name, searchRoot)
		}
	}
	result.searchDirectories = uniquePaths(result.searchDirectories)
	return result
}

func (discovery *pluginDiscovery) addPlugin(pluginBase string, skills []string, name, searchRoot string) {
	if !containedPath(searchRoot, pluginBase) {
		return
	}
	if len(skills) > 0 {
		for _, skillPath := range skills {
			if !validPluginRelative(skillPath) {
				continue
			}
			skillDirectory := secureJoin(pluginBase, skillPath)
			if skillDirectory == "" || !containedPath(searchRoot, skillDirectory) {
				continue
			}
			discovery.searchDirectories = append(discovery.searchDirectories, path.Dir(skillDirectory))
			if name != "" {
				discovery.groupings[skillDirectory] = name
			}
		}
	}
	discovery.searchDirectories = append(discovery.searchDirectories, path.Join(pluginBase, "skills"))
}

func validPluginRelative(value string) bool {
	return value == "" || strings.HasPrefix(value, "./")
}

func secureJoin(base string, components ...string) string {
	joined := base
	for _, component := range components {
		if component == "" {
			continue
		}
		if !strings.HasPrefix(component, "./") {
			return ""
		}
		joined = path.Join(joined, strings.TrimPrefix(component, "./"))
	}
	joined = path.Clean(joined)
	if joined == "." {
		return "."
	}
	if strings.HasPrefix(joined, "../") || joined == ".." || strings.HasPrefix(joined, "/") {
		return ""
	}
	return joined
}

func containedPath(base, target string) bool {
	base = path.Clean(base)
	target = path.Clean(target)
	if base == "." || base == "" {
		return target == "." || !strings.HasPrefix(target, "../") && target != ".." && !strings.HasPrefix(target, "/")
	}
	return target == base || strings.HasPrefix(target, base+"/")
}

func (scan *Scan) walkSkillDirectories(directory string, maxDepth, depth int, seenNames map[string]bool, groupings map[string]string) {
	for _, child := range scan.immediateDirectories(directory) {
		if skippedDirectory(path.Base(child)) {
			continue
		}
		file := scan.repoPath(path.Join(child, "SKILL.md"))
		if scan.hasFile(file) {
			if candidate, ok := scan.readCandidate(file, groupings[scan.repoPath(child)]); ok {
				scan.addCandidate(candidate, seenNames)
			}
			continue
		}
		if depth < maxDepth {
			scan.walkSkillDirectories(child, maxDepth, depth+1, seenNames, groupings)
		}
	}
}

func (scan *Scan) findAllSkillDirectories(directory string, depth int, seenNames map[string]bool, groupings map[string]string) {
	file := scan.repoPath(path.Join(directory, "SKILL.md"))
	if scan.hasFile(file) {
		if candidate, ok := scan.readCandidate(file, groupings[scan.repositorySkillDirectory(directory)]); ok {
			scan.addCandidate(candidate, seenNames)
		}
	}
	if depth >= 5 {
		return
	}
	for _, child := range scan.immediateDirectories(directory) {
		if !skippedDirectory(path.Base(child)) {
			scan.findAllSkillDirectories(child, depth+1, seenNames, groupings)
		}
	}
}

func (scan *Scan) repositorySkillDirectory(searchDirectory string) string {
	directory := scan.repoPath(searchDirectory)
	if directory == "" {
		return "."
	}
	return path.Clean(directory)
}

func (scan *Scan) immediateDirectories(directory string) []string {
	repositoryDirectory := scan.repoPath(directory)
	if repositoryDirectory == "" {
		repositoryDirectory = "."
	}
	searchRoot := scan.searchRoot()
	children := scan.directoryChildren[repositoryDirectory]
	result := make([]string, 0, len(children))
	for _, child := range children {
		if searchRoot != "" {
			if !strings.HasPrefix(child, searchRoot+"/") {
				continue
			}
			child = strings.TrimPrefix(child, searchRoot+"/")
		}
		result = append(result, child)
	}
	return result
}

func (scan *Scan) hasFile(repositoryPath string) bool {
	return scan.indexedFiles[repositoryPath]
}

func (scan *Scan) readCandidate(skillFile, pluginName string) (Candidate, bool) {
	raw, err := scan.readRepositoryFile(skillFile)
	if err != nil {
		return Candidate{}, false
	}
	name, description, internal, ok := parseSkillFrontmatter(string(raw))
	if !ok || (internal && !scan.includeInternal()) {
		return Candidate{}, false
	}
	directory := path.Dir(skillFile)
	if directory == "" {
		directory = "."
	}
	if scan.isInstalledProjectSkill(directory, name) {
		return Candidate{}, false
	}
	return Candidate{
		Name: name, Description: description, Path: directory,
		Content: string(raw), Internal: internal, PluginName: pluginName,
	}, true
}

func (scan *Scan) addCandidate(candidate Candidate, seenNames map[string]bool) {
	if seenNames[candidate.Name] {
		return
	}
	seenNames[candidate.Name] = true
	scan.candidates = append(scan.candidates, candidate)
	scan.byPath[candidate.Path] = candidate
}

func (scan *Scan) includeInternal() bool {
	if scan.options.IncludeInternal {
		return true
	}
	value := strings.ToLower(strings.TrimSpace(os.Getenv("INSTALL_INTERNAL_SKILLS")))
	return value == "1" || value == "true"
}

func (scan *Scan) isInstalledProjectSkill(directory, name string) bool {
	if len(scan.lockedSkillNames) == 0 {
		return false
	}
	directory = path.Clean(directory)
	isAgentSkillPath := false
	for _, agentDirectory := range projectSkillDirectories {
		if directory == agentDirectory || strings.HasPrefix(directory, agentDirectory+"/") {
			isAgentSkillPath = true
			break
		}
	}
	if !isAgentSkillPath {
		return false
	}
	return scan.lockedSkillNames[normalizeSkillName(name)] || scan.lockedSkillNames[normalizeSkillName(path.Base(directory))]
}

func normalizeSkillName(name string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.ToLower(name), "_", " ")), "-")
}

func uniquePaths(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
