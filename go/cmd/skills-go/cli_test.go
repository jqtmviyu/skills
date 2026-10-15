package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		if strings.HasPrefix(variable, "HOME=") || strings.HasPrefix(variable, "XDG_CONFIG_HOME=") || strings.HasPrefix(variable, "XDG_STATE_HOME=") || strings.HasPrefix(variable, "CLAUDE_CONFIG_DIR=") || strings.HasPrefix(variable, "GIT_CONFIG_GLOBAL=") {
			continue
		}
		filtered = append(filtered, variable)
	}
	return filtered
}

func environmentWithOverrides(environment, overrides []string) []string {
	result := append([]string(nil), environment...)
	for _, override := range overrides {
		name, _, _ := strings.Cut(override, "=")
		prefix := name + "="
		filtered := result[:0]
		for _, variable := range result {
			if !strings.HasPrefix(variable, prefix) {
				filtered = append(filtered, variable)
			}
		}
		result = append(filtered, override)
	}
	return result
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

func TestManagementAliasesDispatchToImplementedCommands(t *testing.T) {
	binary := buildCLI(t)
	for _, command := range []string{"ls", "install", "i", "check", "upgrade"} {
		t.Run(command, func(t *testing.T) {
			args := []string{command}
			if command == "check" || command == "upgrade" {
				args = append(args, "--yes")
			}
			stdout, stderr, exitCode := runCLI(t, binary, args...)
			if exitCode != 0 {
				t.Fatalf("%s exit = %d; stdout: %s; stderr: %s", command, exitCode, stdout, stderr)
			}
			if strings.Contains(stderr, "not implemented") {
				t.Fatalf("%s alias did not dispatch: %s", command, stderr)
			}
		})
	}
	for _, command := range []string{"rm", "r"} {
		t.Run(command, func(t *testing.T) {
			_, stderr, exitCode := runCLI(t, binary, command, "missing-skill", "--yes")
			if exitCode != 0 || !strings.Contains(stderr, "no matching skills found") {
				t.Fatalf("%s alias exit/stderr = %d/%q, want compatible missing-skill result", command, exitCode, stderr)
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
	var results []map[string]any
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v; stdout: %s", err, stdout)
	}
	if len(results) != 1 || results[0]["status"] != "failed" || results[0]["error"] != "add requires a source" {
		t.Fatalf("JSON error result = %#v, want one failed result", results)
	}
	if stderr != "skills-go: add requires a source\n" {
		t.Fatalf("stderr = %q, want missing-source error", stderr)
	}

	stdout, stderr, exitCode = runCLI(t, binary, "add", "--metadata", "not-json", "--json")
	if exitCode != 1 {
		t.Fatalf("invalid metadata exit = %d, want 1; stderr: %s", exitCode, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &results); err != nil || len(results) != 1 || results[0]["status"] != "failed" {
		t.Fatalf("invalid metadata JSON output = %s (error %v), want one failed result", stdout, err)
	}
}

func TestAddInstallsSelectedLocalSkillWithSupportingFiles(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	home := filepath.Join(root, "home")
	source := filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha", "scripts"), filepath.Join(source, "skills", "beta")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	writeTestSkill(t, filepath.Join(source, "skills", "beta"), "beta")
	if err := os.WriteFile(filepath.Join(source, "skills", "alpha", "scripts", "helper.sh"), []byte("echo alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	installed := filepath.Join(project, ".claude", "skills", "alpha")
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); err != nil {
		t.Fatalf("selected skill was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed, "scripts", "helper.sh")); err != nil {
		t.Fatalf("selected skill support file was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "beta")); !os.IsNotExist(err) {
		t.Fatalf("unselected skill should not be installed; stat error = %v", err)
	}
}

func TestAddSupportsLocalGitFileURLOffline(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: local git skill\n---\n",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: sibling skill\n---\n",
	})
	source := "file://" + filepath.ToSlash(fixture.Bare) + "#main@alpha"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("local Git add exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("local Git source did not install the selected skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "beta")); !os.IsNotExist(err) {
		t.Fatalf("local Git @skill selector installed its sibling: %v", err)
	}
}

func TestAddSupportsGitLabHTTPSAndSSHSourcesOffline(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: alpha description\n---\n",
	})
	for index, source := range []string{"gitlab:test/repo", "https://git.example.com/team/repo.git", "git@github.com:test/repo.git", "ssh://git@github.com/test/repo.git"} {
		project := filepath.Join(root, "project-"+strings.ReplaceAll(strings.ReplaceAll(source, ":", "-"), "/", "-"))
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--agent", "claude-code", "--copy", "--yes")
		if exitCode != 0 {
			t.Fatalf("source %q add exit = %d; stdout: %s; stderr: %s", source, exitCode, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
			t.Fatalf("source %q did not install fixture skill: %v", source, err)
		}
		if index == 0 {
			if _, err := os.Stat(filepath.Join(fixture.Bare, "objects")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAddGitHubSelectorMaterializesOnlySelectedSkill(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"README.md":                        "unrelated repository documentation\n",
		"skills/alpha/SKILL.md":            "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/guide.md": "alpha reference\n",
		"skills/beta/SKILL.md":             "---\nname: beta\ndescription: beta description\n---\n",
		"skills/beta/references/guide.md":  "beta reference\n",
		"unrelated/large.bin":              strings.Repeat("large unrelated payload ", 4096),
	})

	trace := filepath.Join(root, "git-packets.log")
	stdout, stderr, exitCode := runCLIAtWithEnv(t, binary, project, home, "", []string{"GIT_TRACE_PACKET=" + trace}, "a", "test/repo@alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	installed := filepath.Join(project, ".claude", "skills", "alpha")
	if _, err := os.Stat(filepath.Join(installed, "references", "guide.md")); err != nil {
		t.Fatalf("selected skill support file is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "beta")); !os.IsNotExist(err) {
		t.Fatalf("unselected skill was installed; stat error = %v", err)
	}
	traceData, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("Git packet trace is missing: %v", err)
	}
	for label, contents := range map[string]string{"selected support": "alpha reference\n"} {
		if hash := gitBlobHash(t, contents); !strings.Contains(string(traceData), hash) {
			t.Errorf("Git trace does not show the selected %s blob being requested", label)
		}
	}
	for label, contents := range map[string]string{"unselected support": "beta reference\n", "unrelated large blob": strings.Repeat("large unrelated payload ", 4096)} {
		if hash := gitBlobHash(t, contents); strings.Contains(string(traceData), hash) {
			t.Errorf("Git trace shows the %s blob being transferred before it is selected", label)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.Bare, "objects")); err != nil {
		t.Fatalf("Git fixture object database missing: %v", err)
	}

	globalProject := filepath.Join(root, "global-project")
	if err := os.MkdirAll(globalProject, 0o700); err != nil {
		t.Fatal(err)
	}
	_, stderr, exitCode = runCLIAt(t, binary, globalProject, home, "", "add", "test/repo@alpha", "--agent", "claude-code", "--copy", "--yes", "--global")
	if exitCode != 0 {
		t.Fatalf("global GitHub add exit = %d; stderr: %s", exitCode, stderr)
	}
	globalLock, err := os.ReadFile(filepath.Join(home, ".agents", ".skill-lock.json"))
	if err != nil {
		t.Fatalf("global GitHub lock missing: %v", err)
	}
	var globalDocument struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(globalLock, &globalDocument); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "--git-dir="+fixture.Bare, "rev-parse", "refs/heads/main:skills/alpha")
	expectedTreeHash, err := command.Output()
	if err != nil {
		t.Fatalf("resolve fixture skill tree: %v", err)
	}
	if got := globalDocument.Skills["alpha"]["skillFolderHash"]; got != strings.TrimSpace(string(expectedTreeHash)) {
		t.Errorf("global skillFolderHash = %#v, want Git tree hash %s", got, strings.TrimSpace(string(expectedTreeHash)))
	}
}

func TestAddFallsBackWhenGitServerIgnoresBlobFilter(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":            "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/guide.md": "support",
		"unrelated/large.bin":              strings.Repeat("large payload ", 4096),
	})
	gitTest(t, "", "--git-dir="+fixture.Bare, "config", "uploadpack.allowFilter", "false")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("filter fallback add exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "filter") || !strings.Contains(strings.ToLower(stderr), "continuing") {
		t.Fatalf("stderr does not explain the larger unfiltered fetch: %q", stderr)
	}
	if content, err := os.ReadFile(filepath.Join(project, ".claude", "skills", "alpha", "references", "guide.md")); err != nil || string(content) != "support" {
		t.Fatalf("filter fallback did not install complete skill: %q, error %v", content, err)
	}
}

func TestAddGitTreeURLHonorsRefAndSubpath(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	createGitHubFixture(t, root, home, map[string]string{
		"pkg/skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: selected from subpath\n---\n",
		"pkg/skills/alpha/references/ref.md": "selected support\n",
		"pkg/skills/beta/SKILL.md":           "---\nname: beta\ndescription: other skill\n---\n",
		"outside/skills/gamma/SKILL.md":      "---\nname: gamma\ndescription: outside subpath\n---\n",
	})

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "https://github.com/test/repo/tree/main/pkg", "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes", "--json")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(stdout), &results); err != nil || len(results) != 1 || results[0]["ref"] != "main" || results[0]["source"] != "test/repo" {
		t.Fatalf("ref/subpath JSON result = %s (error %v)", stdout, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "alpha", "references", "ref.md")); err != nil {
		t.Fatalf("selected subpath skill support file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "gamma")); !os.IsNotExist(err) {
		t.Fatalf("skill outside requested subpath was installed: %v", err)
	}
}

func TestAddJSONReportsStableInstallationContract(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "repo")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha"), filepath.Join(source, "skills", "beta")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	writeTestSkill(t, filepath.Join(source, "skills", "beta"), "beta")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--json", "--yes", "--agent", "claude-code", "--skill", "alpha", "--copy")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(results) != 1 {
		t.Fatalf("JSON results = %#v, want one selected skill", results)
	}
	result := results[0]
	for field, want := range map[string]string{"name": "alpha", "status": "installed", "source": source, "scope": "project", "mode": "copy"} {
		if result[field] != want {
			t.Errorf("JSON field %q = %#v, want %q", field, result[field], want)
		}
	}
	if result["security"] != nil || result["ref"] != nil {
		t.Errorf("JSON nullable fields security/ref = %#v/%#v, want null", result["security"], result["ref"])
	}
	if hash, ok := result["hash"].(string); !ok || len(hash) != 64 {
		t.Errorf("JSON hash = %#v, want a SHA-256 hex string", result["hash"])
	}
	if agents, ok := result["agents"].([]any); !ok || len(agents) != 1 || agents[0] != "Claude Code" {
		t.Errorf("JSON agents = %#v, want [Claude Code]", result["agents"])
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty on success", stderr)
	}
}

func TestAddRootSkillPrecedenceFullDepthAndPluginGrouping(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repository := filepath.Join(root, "repository")
	for _, directory := range []string{home, filepath.Join(repository, "assets"), filepath.Join(repository, "skills", "nested", "scripts")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, repository, "repository-root")
	writeTestSkill(t, filepath.Join(repository, "skills", "nested"), "nested")
	if err := os.WriteFile(filepath.Join(repository, "assets", "instructions.md"), []byte("root support"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "skills", "nested", "scripts", "run.sh"), []byte("nested support"), 0o600); err != nil {
		t.Fatal(err)
	}

	defaultProject := filepath.Join(root, "default-project")
	if err := os.MkdirAll(defaultProject, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, defaultProject, home, "", "add", repository, "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("root-skill install exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	rootInstall := filepath.Join(defaultProject, ".claude", "skills", "repository-root")
	if _, err := os.Stat(filepath.Join(rootInstall, "assets", "instructions.md")); err != nil {
		t.Fatalf("root skill support file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultProject, ".claude", "skills", "nested")); !os.IsNotExist(err) {
		t.Fatalf("root skill should take precedence without --full-depth: %v", err)
	}

	fullDepthProject := filepath.Join(root, "full-depth-project")
	if err := os.MkdirAll(fullDepthProject, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, fullDepthProject, home, "", "add", repository, "--full-depth", "--skill", "nested", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("full-depth install exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(fullDepthProject, ".claude", "skills", "nested", "scripts", "run.sh")); err != nil {
		t.Fatalf("full-depth nested skill support missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fullDepthProject, ".claude", "skills", "repository-root")); !os.IsNotExist(err) {
		t.Fatalf("explicit full-depth selection installed the root skill too: %v", err)
	}

	pluginRepo := filepath.Join(root, "plugin-repo")
	pluginSkill := filepath.Join(pluginRepo, "plugins", "docs", "guide")
	if err := os.MkdirAll(pluginSkill, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, pluginSkill, "guide")
	if err := os.MkdirAll(filepath.Join(pluginRepo, ".claude-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"plugins":[{"name":"docs-plugin","source":"./plugins/docs","skills":["./guide"]}]}`
	if err := os.WriteFile(filepath.Join(pluginRepo, ".claude-plugin", "marketplace.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	listProject := filepath.Join(root, "list-project")
	if err := os.MkdirAll(listProject, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, listProject, home, "", "add", pluginRepo, "--list")
	if exitCode != 0 || !strings.Contains(stdout, "Docs Plugin") || !strings.Contains(stdout, "guide — guide description") || stderr != "" {
		t.Fatalf("plugin-grouped list = exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
}

func TestAddInstallsToMultipleEveSubagents(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "eve", "--subagent", "root", "reviewer", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("Eve subagent add exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	for _, directory := range []string{
		filepath.Join(project, "agent", "skills", "alpha"),
		filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha"),
	} {
		if _, err := os.Stat(filepath.Join(directory, "SKILL.md")); err != nil {
			t.Errorf("Eve target %s missing: %v", directory, err)
		}
	}
	lock, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("read project lock: %v", err)
	}
	var lockDocument struct {
		Skills map[string]struct {
			Subagents []string `json:"subagents"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(lock, &lockDocument); err != nil {
		t.Fatal(err)
	}
	gotSubagents := lockDocument.Skills["alpha"].Subagents
	if len(gotSubagents) != 2 || gotSubagents[0] != "" || gotSubagents[1] != "reviewer" {
		t.Errorf("project lock Eve placements = %#v, want [root, reviewer]", gotSubagents)
	}
}

func TestAddMultiSelectionAndGlobalProjectLocks(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, globalProject, home := filepath.Join(root, "project"), filepath.Join(root, "global-project"), filepath.Join(root, "home")
	source := filepath.Join(root, "repo")
	for _, directory := range []string{project, globalProject, home, filepath.Join(source, "skills", "alpha"), filepath.Join(source, "skills", "beta")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	writeTestSkill(t, filepath.Join(source, "skills", "beta"), "beta")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "-s", "alpha", "beta", "-a", "claude-code", "--copy", "-y")
	if exitCode != 0 {
		t.Fatalf("multi-selection exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	projectLock, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("project lock was not written: %v", err)
	}
	var projectDocument struct {
		Version int                       `json:"version"`
		Skills  map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(projectLock, &projectDocument); err != nil {
		t.Fatal(err)
	}
	if projectDocument.Version != 1 || len(projectDocument.Skills) != 2 {
		t.Fatalf("project lock version/skills = %d/%v, want v1 with both skills", projectDocument.Version, projectDocument.Skills)
	}
	computedHash, hashOK := projectDocument.Skills["alpha"]["computedHash"].(string)
	if projectDocument.Skills["alpha"]["sourceType"] != "local" || !hashOK || len(computedHash) != 64 {
		t.Fatalf("project lock entry does not preserve local source/hash contract: %#v", projectDocument.Skills["alpha"])
	}

	yesProject := filepath.Join(root, "yes-project")
	if err := os.MkdirAll(yesProject, 0o700); err != nil {
		t.Fatal(err)
	}
	_, stderr, exitCode = runCLIAt(t, binary, yesProject, home, "", "add", source, "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("--yes auto-selection exit = %d; stderr: %s", exitCode, stderr)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(yesProject, ".claude", "skills", name, "SKILL.md")); err != nil {
			t.Errorf("--yes did not select %s: %v", name, err)
		}
	}

	_, stderr, exitCode = runCLIAt(t, binary, globalProject, home, "", "add", source, "--skill", "beta", "--agent", "claude-code", "--copy", "--yes", "--global")
	if exitCode != 0 {
		t.Fatalf("global add exit = %d; stderr: %s", exitCode, stderr)
	}
	globalSkill := filepath.Join(home, ".claude", "skills", "beta", "SKILL.md")
	if _, err := os.Stat(globalSkill); err != nil {
		t.Fatalf("global skill path %s missing: %v", globalSkill, err)
	}
	globalLock, err := os.ReadFile(filepath.Join(home, ".agents", ".skill-lock.json"))
	if err != nil {
		t.Fatalf("global lock was not written: %v", err)
	}
	var globalDocument struct {
		Version int                       `json:"version"`
		Skills  map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(globalLock, &globalDocument); err != nil {
		t.Fatal(err)
	}
	if globalDocument.Version != 3 || globalDocument.Skills["beta"]["sourceType"] != "local" {
		t.Fatalf("global lock version/entry = %d/%#v, want v3 local entry", globalDocument.Version, globalDocument.Skills["beta"])
	}
	folderHash, folderHashOK := globalDocument.Skills["beta"]["skillFolderHash"].(string)
	installedAt, installedAtOK := globalDocument.Skills["beta"]["installedAt"].(string)
	updatedAt, updatedAtOK := globalDocument.Skills["beta"]["updatedAt"].(string)
	if !folderHashOK || len(folderHash) != 64 || !installedAtOK || !updatedAtOK || installedAt == "" || updatedAt == "" {
		t.Fatalf("global lock hash/timestamps = %#v, want hash and timestamps", globalDocument.Skills["beta"])
	}
}

func TestAddDirectArchiveDownloadsSelectedSkillFiles(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	for filename, content := range map[string]string{
		"review/SKILL.md":                "---\nname: review\ndescription: archive skill\n---\n",
		"review/references/checklist.md": "archive support file",
	} {
		file, err := zipWriter.Create(filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/skills.zip" {
			response.Header().Set("Content-Type", "application/zip")
			_, _ = response.Write(archive.Bytes())
			return
		}
		http.NotFound(response, request)
	}))
	defer server.Close()
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", server.URL+"/skills.zip", "--skill", "review", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("direct archive add exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if content, err := os.ReadFile(filepath.Join(project, ".claude", "skills", "review", "references", "checklist.md")); err != nil || string(content) != "archive support file" {
		t.Fatalf("downloaded support file = %q, error %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("direct download should not create a reproducible source lock: %v", err)
	}
}

func TestAddWellKnownSourceFetchesIndexedSkillAndSupportFiles(t *testing.T) {
	binary := buildCLI(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/agent-skills/index.json":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"skills":[{"name":"alpha","description":"alpha description","files":["SKILL.md","references/guide.md"]}]}`))
		case "/.well-known/agent-skills/alpha/SKILL.md":
			_, _ = response.Write([]byte("---\nname: alpha\ndescription: alpha description\n---\n"))
		case "/.well-known/agent-skills/alpha/references/guide.md":
			_, _ = response.Write([]byte("remote support file"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", server.URL, "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("well-known add exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	content, err := os.ReadFile(filepath.Join(project, ".claude", "skills", "alpha", "references", "guide.md"))
	if err != nil || string(content) != "remote support file" {
		t.Fatalf("well-known support file = %q, error %v", content, err)
	}
	lock, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("well-known add did not write a project lock: %v", err)
	}
	var lockDocument struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(lock, &lockDocument); err != nil {
		t.Fatal(err)
	}
	entry := lockDocument.Skills["alpha"]
	if entry["sourceType"] != "well-known" || entry["sourceBaseUrl"] != server.URL {
		t.Fatalf("well-known lock source fields = %#v", entry)
	}
	if digest, ok := entry["wellKnownDigest"].(string); !ok || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("well-known lock digest = %#v, want sha256 digest", entry["wellKnownDigest"])
	}
}

func TestAddExplicitSelectorIncludesFrontmatterMarkedInternalSkill(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "public"), filepath.Join(source, "skills", "internal")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "public"), "public")
	internal := "---\nname: internal\ndescription: internal-only skill\nmetadata:\n  internal: true\n---\n"
	if err := os.WriteFile(filepath.Join(source, "skills", "internal", "SKILL.md"), []byte(internal), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--list")
	if exitCode != 0 || strings.Contains(stdout, "internal-only skill") || !strings.Contains(stdout, "public description") || stderr != "" {
		t.Fatalf("default discovery output = exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "add", source, "--skill", "internal", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("explicit internal-skill install exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "internal", "SKILL.md")); err != nil {
		t.Fatalf("explicitly selected internal skill was not installed: %v", err)
	}
}

func TestAddNoninteractiveSelectionAndJSONExitContracts(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha"), filepath.Join(source, "skills", "beta")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	writeTestSkill(t, filepath.Join(source, "skills", "beta"), "beta")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--agent", "claude-code", "--copy")
	if exitCode != 1 || !strings.Contains(stderr, "multiple skills found") {
		t.Fatalf("noninteractive add = exit %d, stderr %q; want selection guidance", exitCode, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatalf("noninteractive selection installed files unexpectedly: %v", err)
	}

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--copy")
	if exitCode != 1 || !strings.Contains(stderr, "interactive prompt required") {
		t.Fatalf("noninteractive single-skill install = exit %d; stderr %q; want prompt guidance", exitCode, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatalf("add without --yes installed files unexpectedly: %v", err)
	}

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "add", source, "--json", "--agent", "claude-code")
	if exitCode != 1 {
		t.Fatalf("JSON without --yes exit = %d, want 1; stderr: %s", exitCode, stderr)
	}
	var errorResult []map[string]any
	if err := json.Unmarshal([]byte(stdout), &errorResult); err != nil || len(errorResult) != 1 || errorResult[0]["status"] != "failed" {
		t.Fatalf("JSON noninteractive contract = %s (error %v), want one failed entry", stdout, err)
	}

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "add", source, "--json", "--yes", "--agent", "claude-code", "--skill", "missing")
	if exitCode != 1 {
		t.Fatalf("missing-skill JSON exit = %d, want 1; stderr: %s", exitCode, stderr)
	}
	var skipped []map[string]any
	if err := json.Unmarshal([]byte(stdout), &skipped); err != nil || len(skipped) != 1 || skipped[0]["status"] != "skipped" || skipped[0]["name"] != "missing" {
		t.Fatalf("missing-skill JSON result = %s (error %v), want a skipped record", stdout, err)
	}
	if !strings.Contains(stderr, "missing") {
		t.Errorf("missing-skill diagnostic should be written to stderr, got %q", stderr)
	}
}

func TestAddTargetsAllSeventyEightAgentsAndRepresentativePaths(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--agent", "*", "--skill", "alpha", "--copy", "--yes", "--json")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var results []map[string]any
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("JSON output is invalid: %v; stdout: %s", err, stdout)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v, want one skill result", results)
	}
	agents, ok := results[0]["agents"].([]any)
	if !ok || len(agents) != 78 {
		t.Fatalf("agent results count = %#v, want all 78 supported agents", results[0]["agents"])
	}
	for _, representative := range []string{"AiderDesk", "Claude Code", "Codex", "Eve", "AdaL"} {
		if !containsJSONValue(agents, representative) {
			t.Errorf("agent result list does not contain representative %q", representative)
		}
	}
	for _, directory := range []string{
		filepath.Join(project, ".aider-desk", "skills", "alpha"),
		filepath.Join(project, ".claude", "skills", "alpha"),
		filepath.Join(project, ".agents", "skills", "alpha"),
		filepath.Join(project, "agent", "skills", "alpha"),
		filepath.Join(project, ".adal", "skills", "alpha"),
	} {
		if _, err := os.Stat(directory); err != nil {
			t.Errorf("representative agent target %s missing: %v", directory, err)
		}
	}

	allProject := filepath.Join(root, "all-project")
	if err := os.MkdirAll(allProject, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, allProject, home, "", "add", source, "--all", "--json")
	if exitCode != 0 {
		t.Fatalf("--all exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &results); err != nil || len(results) != 1 || results[0]["status"] != "installed" {
		t.Fatalf("--all JSON result = %s (error %v)", stdout, err)
	}
	allAgents, ok := results[0]["agents"].([]any)
	if !ok || len(allAgents) != 78 || results[0]["mode"] != "symlink" {
		t.Fatalf("--all agent/mode results = %#v/%#v", results[0]["agents"], results[0]["mode"])
	}
}

func containsJSONValue(values []any, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestUseGeneratesPromptWithSelectedSkillSupportDirectory(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "repo")
	alpha := filepath.Join(source, "skills", "alpha")
	beta := filepath.Join(source, "skills", "beta")
	for _, directory := range []string{project, home, filepath.Join(alpha, "references"), beta} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, alpha, "alpha")
	writeTestSkill(t, beta, "beta")
	if err := os.WriteFile(filepath.Join(alpha, "references", "guide.md"), []byte("alpha support"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedSupport := filepath.Join(root, "linked-support")
	if err := os.MkdirAll(linkedSupport, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedSupport, "linked.md"), []byte("dereferenced support"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedSupport, filepath.Join(alpha, "linked")); err != nil {
		t.Skipf("create support directory symlink: %v", err)
	}

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "use", source, "-s", "alpha")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "<SKILL.md>\n---\nname: alpha") || strings.Contains(stdout, "name: beta") {
		t.Fatalf("use prompt did not contain only selected SKILL.md: %s", stdout)
	}
	marker := "Supporting files for this skill were downloaded to:\n"
	parts := strings.SplitN(stdout, marker, 2)
	if len(parts) != 2 {
		t.Fatalf("use prompt does not mention its support directory: %s", stdout)
	}
	supportDir := strings.Fields(parts[1])[0]
	defer os.RemoveAll(filepath.Dir(supportDir))
	if content, err := os.ReadFile(filepath.Join(supportDir, "references", "guide.md")); err != nil || string(content) != "alpha support" {
		t.Fatalf("support file = %q, error %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(supportDir, "linked", "linked.md")); err != nil || string(content) != "dereferenced support" {
		t.Fatalf("dereferenced symlink support = %q, error %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(supportDir, "../beta", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unselected sibling skill is in use support directory: %v", err)
	}
}

func TestUseLaunchesSelectedAgentAndPropagatesItsExitCode(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home, source := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "source")
	for _, directory := range []string{project, home, filepath.Join(source, "skills", "alpha")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, filepath.Join(source, "skills", "alpha"), "alpha")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "agent-prompt.txt")
	fakeAgent := filepath.Join(bin, "claude")
	writeAgent := func(exitCode string) {
		t.Helper()
		script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$SKILLS_TEST_AGENT_CAPTURE\"\nexit " + exitCode + "\n"
		if err := os.WriteFile(fakeAgent, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fakeAgent, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeAgent("0")

	stdout, stderr, exitCode := runCLIAtWithEnv(t, binary, project, home, "", []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SKILLS_TEST_AGENT_CAPTURE=" + capture}, "use", source, "--skill", "alpha", "--agent", "claude-code")
	if exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("agent use = exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	prompt, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(prompt), "<SKILL.md>\n---\nname: alpha") {
		t.Fatalf("launched agent prompt = %q, error %v", prompt, err)
	}

	writeAgent("17")
	_, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SKILLS_TEST_AGENT_CAPTURE=" + capture}, "use", source, "--skill", "alpha", "--agent", "claude-code")
	if exitCode != 17 {
		t.Fatalf("agent exit code = %d, want propagated code 17; stderr: %s", exitCode, stderr)
	}
}

func TestInitCreatesCompatibleSkillTemplate(t *testing.T) {
	binary := buildCLI(t)
	project := t.TempDir()
	home := filepath.Join(project, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "init", "review-skill")
	if exitCode != 0 {
		t.Fatalf("exit code = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	content, err := os.ReadFile(filepath.Join(project, "review-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("init did not create SKILL.md: %v", err)
	}
	if !strings.Contains(string(content), "name: review-skill\ndescription: A brief description of what this skill does") || !strings.Contains(string(content), "## When to use") || !strings.Contains(stdout, "review-skill/SKILL.md") {
		t.Fatalf("init output/template does not match the compatible template:\n%s\n%s", stdout, content)
	}
	second, _, secondExit := runCLIAt(t, binary, project, home, "", "init", "review-skill")
	if secondExit != 0 || !strings.Contains(second, "Skill already exists at review-skill/SKILL.md") {
		t.Fatalf("second init = exit %d, output %q; want existing-skill message", secondExit, second)
	}
}

type blackBoxGitFixture struct {
	Bare string
}

func createGitHubFixture(t *testing.T, root, home string, files map[string]string) blackBoxGitFixture {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "git-work")
	bare := filepath.Join(root, "fixture.git")
	gitTest(t, "", "init", "--bare", "-q", "-b", "main", bare)
	gitTest(t, "", "init", "-q", "-b", "main", working)
	gitTest(t, working, "config", "user.name", "Skills Test")
	gitTest(t, working, "config", "user.email", "skills-test@example.invalid")
	for relative, content := range files {
		filename := filepath.Join(working, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitTest(t, working, "add", "--all")
	gitTest(t, working, "commit", "-qm", "fixture")
	gitTest(t, working, "remote", "add", "origin", bare)
	gitTest(t, working, "push", "-q", "origin", "main:main")
	gitTest(t, "", "--git-dir="+bare, "config", "uploadpack.allowFilter", "true")
	gitTest(t, "", "--git-dir="+bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	config := "[url \"file://" + filepath.ToSlash(bare) + "\"]\n\tinsteadOf = https://github.com/test/repo.git\n\tinsteadOf = https://gitlab.com/test/repo.git\n\tinsteadOf = https://git.example.com/team/repo.git\n\tinsteadOf = git@github.com:test/repo.git\n\tinsteadOf = ssh://git@github.com/test/repo.git\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return blackBoxGitFixture{Bare: bare}
}

func gitBlobHash(t *testing.T, contents string) string {
	t.Helper()
	command := exec.Command("git", "hash-object", "--stdin")
	command.Stdin = strings.NewReader(contents)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("hash Git fixture blob: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func gitTest(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runCLIAt(t *testing.T, binary, project, home, input string, args ...string) (string, string, int) {
	return runCLIAtWithEnv(t, binary, project, home, input, nil, args...)
}

func runCLIAtWithEnv(t *testing.T, binary, project, home, input string, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = project
	command.Env = environmentWithOverrides(append(environmentWithoutHome(os.Environ()), "HOME="+home), extraEnv)
	command.Stdin = strings.NewReader(input)
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

func writeTestSkill(t *testing.T, directory, name string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + name + " description\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
