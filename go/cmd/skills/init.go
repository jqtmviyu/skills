package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const initSkillTemplate = `---
name: %s
description: A brief description of what this skill does
---

# %s

Instructions for the agent to follow when this skill is activated.

## When to use

Describe when this skill should be used.

## Instructions

1. First step
2. Second step
3. Additional steps as needed
`

func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, "Usage: skills init [name]")
		return 0
	}
	if len(args) > 1 {
		fmt.Fprintln(stderr, "skills: init accepts at most one skill name")
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "skills: resolve project directory: %v\n", err)
		return 1
	}
	name := filepath.Base(cwd)
	hasName := len(args) == 1
	if hasName {
		name = args[0]
		if filepath.IsAbs(name) || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "\\") {
			fmt.Fprintln(stderr, "skills: skill name must be a single path component")
			return 1
		}
	}
	directory := cwd
	displayPath := "SKILL.md"
	if hasName {
		directory = filepath.Join(cwd, name)
		displayPath = filepath.ToSlash(filepath.Join(name, "SKILL.md"))
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		fmt.Fprintf(stderr, "skills: create skill directory: %v\n", err)
		return 1
	}
	filename := filepath.Join(directory, "SKILL.md")
	if _, err := os.Stat(filename); err == nil {
		fmt.Fprintf(stdout, "Skill already exists at %s\n", displayPath)
		return 0
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "skills: inspect %s: %v\n", displayPath, err)
		return 1
	}
	if err := os.WriteFile(filename, []byte(fmt.Sprintf(initSkillTemplate, name, name)), 0o644); err != nil {
		fmt.Fprintf(stderr, "skills: create %s: %v\n", displayPath, err)
		return 1
	}
	fmt.Fprintf(stdout, `Initialized skill: %s

Created:
  %s

Next steps:
  1. Edit %s to define your skill instructions
  2. Update the name and description in the frontmatter

Publishing:
  GitHub:  Push to a repo, then npx skills add <owner>/<repo>
  URL:     Host the file, then npx skills add https://example.com/%s

Browse existing skills for inspiration at https://skills.sh/
`, name, displayPath, displayPath, displayPath)
	return 0
}
