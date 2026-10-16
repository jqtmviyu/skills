package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
	"github.com/vercel-labs/skills/go/installer"
	"github.com/vercel-labs/skills/go/lockfile"
	"github.com/vercel-labs/skills/go/skillscan"
)

type restoreEntry struct {
	name  string
	entry lockfile.Entry
}

type restoreGroup struct {
	input   string
	entries []restoreEntry
}

func runRestore(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "skills: experimental_install does not accept arguments: %s\n", args[0])
		return 1
	}
	cwd, err := currentDirectory()
	if err != nil {
		fmt.Fprintf(stderr, "skills: %v\n", err)
		return 1
	}
	lock, err := lockfile.ReadProject(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "skills: read project lock: %v\n", err)
		return 1
	}
	if len(lock.Skills) == 0 {
		fmt.Fprintln(stdout, "No project skills found in skills-lock.json.")
		return 0
	}
	groups := make(map[string]*restoreGroup)
	var groupOrder []string
	failed := false
	for name, entry := range lock.Skills {
		sourceType, _ := entry.String("sourceType")
		if sourceType == "node_modules" {
			fmt.Fprintf(stderr, "skills: skipping %s because package sync is not supported\n", name)
			continue
		}
		input, sourceErr := restoreScanInput(entry)
		if sourceErr != nil {
			fmt.Fprintf(stderr, "skills: cannot restore %s: %v\n", name, sourceErr)
			failed = true
			continue
		}
		if group := groups[input]; group != nil {
			group.entries = append(group.entries, restoreEntry{name: name, entry: entry})
		} else {
			groups[input] = &restoreGroup{input: input, entries: []restoreEntry{{name: name, entry: entry}}}
			groupOrder = append(groupOrder, input)
		}
	}
	for _, input := range groupOrder {
		group := groups[input]
		session, openErr := openAddSource(context.Background(), input, skillscan.Options{FullDepth: true, IncludeInternal: true, IncludeDuplicateNames: true})
		if openErr != nil {
			fmt.Fprintf(stderr, "skills: cannot scan restore source %s: %v\n", input, openErr)
			failed = true
			continue
		}
		candidates := session.scan.Candidates()
		selected := make([]skillscan.Candidate, 0, len(group.entries))
		for _, item := range group.entries {
			candidate, resolveErr := resolveRestoreCandidate(item, candidates)
			if resolveErr != nil {
				fmt.Fprintf(stderr, "skills: cannot restore %s: %v\n", item.name, resolveErr)
				failed = true
				continue
			}
			selected = append(selected, candidate)
		}
		if len(selected) > 0 {
			paths := make([]string, 0, len(selected))
			for _, candidate := range selected {
				paths = append(paths, candidate.Path)
			}
			materialized, materializeErr := session.scan.Materialize(paths)
			if materializeErr != nil {
				fmt.Fprintf(stderr, "skills: materialize restore source %s: %v\n", input, materializeErr)
				failed = true
			} else {
				byPath := make(map[string]skillscan.MaterializedSkill, len(materialized))
				for _, skill := range materialized {
					byPath[skill.Candidate.Path] = skill
				}
				for _, candidate := range selected {
					skill := byPath[candidate.Path]
					_, installErr := installer.InstallSkill(installer.Skill{Name: candidate.Name, Path: skill.Directory, PluginName: candidate.PluginName}, agent.UniversalID, installer.Options{CWD: cwd, Mode: installer.Symlink, Warnings: stderr})
					if installErr != nil {
						fmt.Fprintf(stderr, "skills: restore %s: %v\n", candidate.Name, installErr)
						failed = true
						continue
					}
					fmt.Fprintf(stdout, "Restored %s to .agents/skills/\n", candidate.Name)
				}
			}
		}
		session.Close()
	}
	if failed {
		return 1
	}
	return 0
}

func restoreScanInput(entry lockfile.Entry) (string, error) {
	sourceType, _ := entry.String("sourceType")
	source, hasSource := entry.String("source")
	if sourceType == "local" {
		if !hasSource || source == "" {
			return "", fmt.Errorf("lock entry is missing its local source path")
		}
		return source, nil
	}
	sourceURL, hasURL := entry.String("sourceUrl")
	if hasURL && sourceURL != "" {
		source = sourceURL
		hasSource = true
	} else if sourceType == "github" && hasSource && isGitHubShorthand(source) {
		source = "https://github.com/" + strings.TrimSuffix(source, ".git") + ".git"
		hasSource = true
	} else if (sourceType == "git" || sourceType == "gitlab") && hasSource && isBareRepositoryShorthand(source) {
		return "", fmt.Errorf("generic Git lock entry has an ambiguous source and no sourceUrl")
	}
	if !hasSource || source == "" {
		return "", fmt.Errorf("lock entry is missing its Git source URL")
	}
	ref, _ := entry.String("ref")
	if ref != "" {
		source += "#" + ref
	}
	return source, nil
}

func isGitHubShorthand(source string) bool {
	return !strings.Contains(source, ":") && strings.Count(strings.TrimSuffix(source, ".git"), "/") == 1
}

func isBareRepositoryShorthand(source string) bool {
	return !strings.Contains(source, ":") && !strings.HasPrefix(source, ".") && !strings.HasPrefix(source, "/")
}

func resolveRestoreCandidate(item restoreEntry, candidates []skillscan.Candidate) (skillscan.Candidate, error) {
	if skillPath, ok := item.entry.String("skillPath"); ok && skillPath != "" {
		folder := path.Clean(strings.TrimSuffix(strings.ReplaceAll(skillPath, "\\", "/"), "/SKILL.md"))
		if folder == "SKILL.md" || folder == "" {
			folder = "."
		}
		for _, candidate := range candidates {
			if path.Clean(candidate.Path) == folder {
				return candidate, nil
			}
		}
	}
	var matches []skillscan.Candidate
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Name, item.name) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return skillscan.Candidate{}, fmt.Errorf("skill %q was not found at its locked path or by name", item.name)
	}
	return skillscan.Candidate{}, fmt.Errorf("skill %q has multiple matching paths", item.name)
}

func currentDirectory() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	return cwd, nil
}
