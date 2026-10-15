package main

import (
	"fmt"
	"io"
	"os"
)

const version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		printHelp(stdout)
		return 0
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(stdout, "skills-go %s\n", version)
		return 0
	}

	commands := map[string]string{
		"add": "add", "a": "add",
		"use":    "use",
		"remove": "remove", "rm": "remove", "r": "remove",
		"list": "list", "ls": "list",
		"update": "update", "check": "update", "upgrade": "update",
		"experimental_install": "experimental_install", "install": "experimental_install", "i": "experimental_install",
		"init": "init",
	}
	canonical, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "skills-go: unknown command %q\n", args[0])
		return 2
	}
	rest := args[1:]
	if containsHelpFlag(rest) {
		switch canonical {
		case "add":
			printAddHelp(stdout)
		case "use":
			printUseHelp(stdout)
		case "init":
			fmt.Fprintln(stdout, "Usage: skills-go init [name]")
		default:
			printHelp(stdout)
		}
		return 0
	}
	switch canonical {
	case "add":
		return runAdd(rest, stdout, stderr)
	case "use":
		return runUse(rest, stdout, stderr)
	case "init":
		return runInit(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "skills-go: command %q is not implemented yet\n", canonical)
		return 1
	}
}

func containsHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `Usage: skills-go <command> [options]

Core commands:
  add <source>          Add skills from a source (alias: a)
  use <source>@<skill>  Use a skill without installing it
  remove [skills]       Remove installed skills (alias: rm)
  list                  List installed skills (alias: ls)
  update [skills...]    Update installed skills (aliases: check, upgrade)
  experimental_install  Restore skills from skills-lock.json (alias: install)
  init [name]           Create a SKILL.md template

Options:
  --help, -h     Show this help message
  --version, -v  Show version number
`)
}

func printAddHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `Usage: skills-go add <source> [options]

Options:
  -g, --global           Install globally
  -p, --project          Install in the current project (default)
  -a, --agent <agents>   Select one or more agents; use '*' for all supported agents
  -s, --skill <skills>   Select one or more skill names; use '*' for all skills
  -l, --list             List available skills without installing
  -y, --yes              Skip prompts and select all discovered skills when unspecified
  --all                  Shorthand for --skill '*' --agent '*' -y
  --copy                 Copy files instead of symlinking agent directories
  --subagent <names>     Install to Eve subagents ('root' selects the root agent)
  --full-depth           Search all nested directories
  --json                 Emit the add result as a JSON array (requires -y)
  --metadata <json>      Validate JSON metadata for compatibility
  --help, -h             Show this help message
`)
}
