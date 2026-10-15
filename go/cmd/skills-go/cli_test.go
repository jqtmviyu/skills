package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildCLI(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "skills-go")
	command := exec.Command("go", "build", "-o", binary, ".")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build skills-go: %v\n%s", err, output)
	}
	return binary
}

func runCLI(t *testing.T, binary string, args ...string) (string, string, int) {
	t.Helper()

	project := t.TempDir()
	home := filepath.Join(project, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatalf("create isolated HOME: %v", err)
	}

	command := exec.Command(binary, args...)
	command.Dir = project
	command.Env = append(environmentWithoutHome(os.Environ()), "HOME="+home)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), exitError.ExitCode()
	}
	t.Fatalf("run skills-go: %v", err)
	return "", "", -1
}

func environmentWithoutHome(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, "HOME=") {
			filtered = append(filtered, variable)
		}
	}
	return filtered
}

func TestVersionFlagReportsVersion(t *testing.T) {
	binary := buildCLI(t)

	stdout, stderr, exitCode := runCLI(t, binary, "--version")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", exitCode, stderr)
	}
	if stdout != "skills-go dev\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "skills-go dev\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestHelpListsSupportedCommands(t *testing.T) {
	binary := buildCLI(t)

	stdout, stderr, exitCode := runCLI(t, binary, "--help")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", exitCode, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, command := range []string{"add", "use", "remove", "list", "update", "check", "upgrade", "experimental_install", "init"} {
		if !strings.Contains(stdout, command) {
			t.Errorf("help does not list command %q", command)
		}
	}
	for _, excluded := range []string{"find", "experimental_sync"} {
		if strings.Contains(stdout, excluded) {
			t.Errorf("help includes out-of-scope command %q", excluded)
		}
	}
}

func TestCoreCommandsAndAliasesAreRouted(t *testing.T) {
	binary := buildCLI(t)
	commands := []struct {
		name      string
		canonical string
	}{
		{"add", "add"}, {"a", "add"},
		{"use", "use"},
		{"remove", "remove"}, {"rm", "remove"}, {"r", "remove"},
		{"list", "list"}, {"ls", "list"},
		{"update", "update"}, {"check", "update"}, {"upgrade", "update"},
		{"experimental_install", "experimental_install"}, {"install", "experimental_install"}, {"i", "experimental_install"},
		{"init", "init"},
	}

	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			args := []string{command.name}
			if command.canonical == "add" {
				args = append(args, "fixture-source")
			}
			stdout, stderr, exitCode := runCLI(t, binary, args...)
			if exitCode != 1 {
				t.Fatalf("exit code = %d, want 1; stderr: %s", exitCode, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			wantError := "skills-go: command \"" + command.canonical + "\" is not implemented yet\n"
			if stderr != wantError {
				t.Fatalf("stderr = %q, want %q", stderr, wantError)
			}
		})
	}
}

func TestUnknownCommandIsReportedOnStderr(t *testing.T) {
	binary := buildCLI(t)

	stdout, stderr, exitCode := runCLI(t, binary, "not-a-command")
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; stderr: %s", exitCode, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "skills-go: unknown command \"not-a-command\"\n" {
		t.Fatalf("stderr = %q, want unknown-command error", stderr)
	}
}

func TestAddJSONModeKeepsStdoutParseableOnMissingSource(t *testing.T) {
	binary := buildCLI(t)

	stdout, stderr, exitCode := runCLI(t, binary, "add", "--json")
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", exitCode, stderr)
	}
	if stdout != "[]\n" {
		t.Fatalf("stdout = %q, want a JSON empty array", stdout)
	}
	if stderr != "skills-go: add requires a source\n" {
		t.Fatalf("stderr = %q, want missing-source error", stderr)
	}
}
