package main

import (
	"fmt"
	"io"
	"os"
	"strings"
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
	if canonical == "add" {
		return runAdd(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "skills-go: command %q is not implemented yet\n", canonical)
	return 1
}

func runAdd(args []string, stdout, stderr io.Writer) int {
	jsonMode := false
	hasSource := false
	for _, arg := range args {
		if arg == "--json" {
			jsonMode = true
		}
		if !strings.HasPrefix(arg, "-") {
			hasSource = true
		}
	}
	if !hasSource {
		if jsonMode {
			fmt.Fprintln(stdout, "[]")
		}
		fmt.Fprintln(stderr, "skills-go: add requires a source")
		return 1
	}
	fmt.Fprintln(stderr, `skills-go: command "add" is not implemented yet`)
	return 1
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
