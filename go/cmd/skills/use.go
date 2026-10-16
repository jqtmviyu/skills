package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
	"github.com/vercel-labs/skills/go/skillscan"
)

var useAgentCommands = map[agent.ID]string{
	"claude-code": "claude",
	"codex":       "codex",
	"sarvam-code": "sarvam-code",
}

func runUse(args []string, stdout, stderr io.Writer) int {
	var source string
	var selector string
	var launchAgent agent.ID
	fullDepth := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			printUseHelp(stdout)
			return 0
		case "--full-depth":
			fullDepth = true
		case "-s", "--skill":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return useFailure(stderr, fmt.Sprintf("%s requires a skill name", arg))
			}
			if selector != "" {
				return useFailure(stderr, "only one --skill value can be provided")
			}
			index++
			selector = args[index]
		case "-a", "--agent":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return useFailure(stderr, fmt.Sprintf("%s requires an agent name", arg))
			}
			if launchAgent != "" {
				return useFailure(stderr, "skills use --agent accepts exactly one agent")
			}
			index++
			launchAgent = agent.ID(args[index])
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				return useFailure(stderr, "skills use --agent accepts exactly one agent")
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return useFailure(stderr, "unknown option: "+arg)
			}
			if source != "" {
				return useFailure(stderr, "expected one source, received multiple arguments")
			}
			source = arg
		}
	}
	if source == "" {
		return useFailure(stderr, "missing required argument: source")
	}
	if launchAgent != "" {
		if _, ok := useAgentCommands[launchAgent]; !ok {
			return useFailure(stderr, fmt.Sprintf("running %s is not supported; supported agents: claude-code, codex, sarvam-code", launchAgent))
		}
	}

	includeInternal := selector != ""
	if parsed, parseErr := skillscan.ParseSource(source); parseErr == nil && parsed.SkillFilter != "" {
		includeInternal = true
	}
	session, err := openAddSource(context.Background(), source, skillscan.Options{FullDepth: fullDepth, IncludeInternal: includeInternal})
	if err != nil {
		return useFailure(stderr, err.Error())
	}
	defer session.Close()
	scan := session.scan
	if status := scan.FilterStatus(); status.Message != "" {
		fmt.Fprintf(stderr, "Warning: %s\n", status.Message)
	}
	if scan.Source().SkillFilter != "" {
		if selector != "" && !strings.EqualFold(selector, scan.Source().SkillFilter) {
			return useFailure(stderr, fmt.Sprintf("conflicting skill selectors: source selects %q but --skill selects %q", scan.Source().SkillFilter, selector))
		}
		selector = scan.Source().SkillFilter
	}
	candidates := scan.Candidates()
	if len(candidates) == 0 {
		return useFailure(stderr, "no valid skills found. Skills require a SKILL.md with name and description.")
	}
	var selected skillscan.Candidate
	if selector == "" {
		if len(candidates) != 1 {
			var names []string
			for _, candidate := range candidates {
				names = append(names, "  - "+candidate.Name)
			}
			return useFailure(stderr, "this source contains multiple skills. Specify exactly one skill:\n"+strings.Join(names, "\n"))
		}
		selected = candidates[0]
	} else {
		matches := matchingCandidates(candidates, selector)
		if len(matches) == 0 {
			var names []string
			for _, candidate := range candidates {
				names = append(names, "  - "+candidate.Name)
			}
			return useFailure(stderr, "no matching skill found for: "+selector+"\nAvailable skills:\n"+strings.Join(names, "\n"))
		}
		if len(matches) != 1 {
			return useFailure(stderr, fmt.Sprintf("skill selector %q matched multiple skills", selector))
		}
		selected = matches[0]
	}

	materialized, err := scan.Materialize([]string{selected.Path})
	if err != nil {
		return useFailure(stderr, err.Error())
	}
	if len(materialized) != 1 {
		return useFailure(stderr, "selected skill could not be materialized")
	}
	useRoot, err := os.MkdirTemp("", "skills-use-")
	if err != nil {
		return useFailure(stderr, fmt.Sprintf("create use directory: %v", err))
	}
	skillDir := filepath.Join(useRoot, safeUseDirectory(selected.Name))
	if err := copyUseDirectory(materialized[0].Directory, skillDir); err != nil {
		_ = os.RemoveAll(useRoot)
		return useFailure(stderr, fmt.Sprintf("materialize support files: %v", err))
	}
	prompt, hasSupport, err := buildSkillPrompt(selected.Content, skillDir)
	if err != nil {
		_ = os.RemoveAll(useRoot)
		return useFailure(stderr, err.Error())
	}
	if hasSupport {
		prompt += fmt.Sprintf("\nSupporting files for this skill were downloaded to:\n%s\n\nWhen the SKILL.md references relative paths, read them from that directory.\n", skillDir)
	}
	if launchAgent != "" {
		command := exec.Command(useAgentCommands[launchAgent], prompt)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				return exitError.ExitCode()
			}
			return useFailure(stderr, err.Error())
		}
		return 0
	}
	_, err = io.WriteString(stdout, prompt)
	if err != nil {
		return useFailure(stderr, err.Error())
	}
	return 0
}

func buildSkillPrompt(skillMD, skillDir string) (string, bool, error) {
	var hasSupportingFiles bool
	err := filepath.WalkDir(skillDir, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(skillDir, filename)
		if err != nil {
			return err
		}
		if !strings.EqualFold(filepath.ToSlash(relative), "skill.md") {
			hasSupportingFiles = true
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	prompt := "You are being given a Skill to execute for the user's next request.\n\nUse the following SKILL.md as your instructions:\n\n<SKILL.md>\n" + skillMD + "\n</SKILL.md>\n"
	return prompt, hasSupportingFiles, nil
}

func copyUseDirectory(source, destination string) error {
	return copyUseTree(source, destination, make(map[string]bool))
}

func copyUseTree(source, destination string, active map[string]bool) error {
	info, err := os.Stat(source)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return copyUseFile(source, destination, info)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if active[resolved] {
		return fmt.Errorf("recursive directory symlink at %s", source)
	}
	active[resolved] = true
	defer delete(active, resolved)
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "metadata.json" || (entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "__pycache__" || entry.Name() == "__pypackages__")) {
			continue
		}
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		childInfo, err := os.Stat(sourcePath)
		if err != nil {
			if entry.Type()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		if childInfo.IsDir() {
			if err := copyUseTree(sourcePath, destinationPath, active); err != nil {
				return err
			}
			continue
		}
		if err := copyUseFile(sourcePath, destinationPath, childInfo); err != nil {
			return err
		}
	}
	return nil
}

func copyUseFile(source, destination string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, info.Mode().Perm())
}

func safeUseDirectory(name string) string {
	return agent.SanitizeInstallName(name)
}

func useFailure(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "skills: %s\n", message)
	return 1
}

func printUseHelp(output io.Writer) {
	fmt.Fprint(output, `Usage: skills use <source>[@<skill>] [options]

Generate a prompt for using one skill without installing it.

Options:
  -s, --skill <skill>  Select the skill to use
  -a, --agent <agent>  Start claude-code, codex, or sarvam-code interactively
  --full-depth         Search nested directories like add --full-depth
  -h, --help           Show this help message
`)
}
