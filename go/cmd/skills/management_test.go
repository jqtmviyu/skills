package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestListReportsInstalledAgentsAndLockMetadataWithoutRewritingLock(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	source := filepath.Join(root, "source", "skills", "alpha")
	for _, directory := range []string{project, home, source, filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, source, "alpha")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", filepath.Dir(filepath.Dir(source)), "--skill", "alpha", "--agent", "claude-code", "codex", "--yes")
	if exitCode != 0 {
		t.Fatalf("install shared skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lockPath := filepath.Join(project, "skills-lock.json")
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read project lock: %v", err)
	}
	lock = []byte(strings.TrimSpace(string(lock)))
	lock = []byte(strings.TrimSuffix(string(lock), "}") + `,"futureField":{"keep":true}}`)
	if err := os.WriteFile(lockPath, lock, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "list", "--json")
	if exitCode != 0 {
		t.Fatalf("list exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var listed []struct {
		Name       string   `json:"name"`
		Path       string   `json:"path"`
		Scope      string   `json:"scope"`
		Agents     []string `json:"agents"`
		Source     string   `json:"source"`
		SourceURL  string   `json:"sourceUrl"`
		SourceType string   `json:"sourceType"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("list output is not JSON: %v; stdout: %s", err, stdout)
	}
	if len(listed) != 1 {
		t.Fatalf("listed skills = %#v, want one shared skill", listed)
	}
	item := listed[0]
	if item.Name != "alpha" || item.Scope != "project" || item.Path != filepath.Join(project, ".agents", "skills", "alpha") {
		t.Errorf("listed skill identity = %#v, want canonical project location", item)
	}
	if strings.Join(item.Agents, ",") != "Claude Code,Codex" {
		t.Errorf("listed agents = %#v, want Claude Code and Codex", item.Agents)
	}
	if item.Source != filepath.Dir(filepath.Dir(source)) || item.SourceType != "local" || item.SourceURL != "" {
		t.Errorf("listed lock metadata = %#v, want original local source and no Git URL", item)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("read-only list rewrote skills-lock.json")
	}
}

func TestRemoveSpecificAgentKeepsSharedCanonicalSkillAndLockUntilLastAgent(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	sourceSkill := filepath.Join(root, "source", "skills", "alpha")
	for _, directory := range []string{project, home, filepath.Join(home, ".codex"), sourceSkill} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, sourceSkill, "alpha")
	source := filepath.Dir(filepath.Dir(sourceSkill))
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "codex", "--yes")
	if exitCode != 0 {
		t.Fatalf("install shared skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	canonical := filepath.Join(project, ".agents", "skills", "alpha")
	claude := filepath.Join(project, ".claude", "skills", "alpha")
	lockPath := filepath.Join(project, "skills-lock.json")

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "remove", "alpha", "--agent", "claude-code", "--yes")
	if exitCode != 0 {
		t.Fatalf("remove Claude Code target: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Lstat(claude); !os.IsNotExist(err) {
		t.Errorf("Claude Code link still exists after removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
		t.Errorf("shared canonical skill was removed while Codex still uses it: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock was removed while another agent still uses the skill: %v", err)
	}

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "remove", "alpha", "--agent", "codex", "--yes")
	if exitCode != 0 {
		t.Fatalf("remove last shared target: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("canonical skill remains after last agent removal: %v", err)
	}
	var lockDocument struct {
		Skills map[string]json.RawMessage `json:"skills"`
	}
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read empty project lock: %v", err)
	}
	if err := json.Unmarshal(lock, &lockDocument); err != nil {
		t.Fatal(err)
	}
	if _, exists := lockDocument.Skills["alpha"]; exists {
		t.Error("last removal left a stale lock entry")
	}

	lockDocument.Skills = map[string]json.RawMessage{"ghost": json.RawMessage(`{"source":"old/repo","sourceType":"github"}`)}
	if err := os.WriteFile(lockPath, mustJSON(t, map[string]any{"version": 1, "skills": lockDocument.Skills}), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "remove", "ghost", "--yes")
	if exitCode != 0 {
		t.Fatalf("remove stale lock entry: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lock, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lockDocument.Skills = nil
	if err := json.Unmarshal(lock, &lockDocument); err != nil {
		t.Fatal(err)
	}
	if _, exists := lockDocument.Skills["ghost"]; exists {
		t.Errorf("removing stale skill did not remove its lock entry; output=%s stderr=%s lock=%s", stdout, stderr, lock)
	}
}

func TestGlobalListAndRemoveHandleLegacyUniversalAgentPaths(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	legacySkill := filepath.Join(configHome, "agents", "skills", "legacy-alpha")
	for _, directory := range []string{project, home, legacySkill, filepath.Join(home, ".agents")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, legacySkill, "legacy-alpha")
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	lock := map[string]any{
		"version":     3,
		"futureField": map[string]any{"preserved": true},
		"skills":      map[string]any{"legacy-alpha": map[string]any{"source": "legacy/repo", "sourceType": "github"}},
	}
	if err := os.WriteFile(lockPath, mustJSON(t, lock), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := []string{"XDG_CONFIG_HOME=" + configHome}

	stdout, stderr, exitCode := runCLIAtWithEnv(t, binary, project, home, "", environment, "list", "--global", "--json")
	if exitCode != 0 {
		t.Fatalf("global list exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var listed []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("global list is not JSON: %v; stdout: %s", err, stdout)
	}
	if len(listed) != 1 || listed[0].Name != "legacy-alpha" {
		t.Fatalf("global list = %#v, want the legacy agent-specific copy", listed)
	}

	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", environment, "remove", "legacy-alpha", "--global", "--agent", "amp", "--yes")
	if exitCode != 0 {
		t.Fatalf("remove Amp legacy path exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(legacySkill, "SKILL.md")); err != nil {
		t.Fatalf("legacy copy shared with Replit was removed: %v", err)
	}

	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", environment, "remove", "--global", "--all")
	if exitCode != 0 {
		t.Fatalf("remove all legacy copies exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(legacySkill); !os.IsNotExist(err) {
		t.Fatalf("safe removal left legacy global copy: %v", err)
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read updated Global lock: %v", err)
	}
	var updated map[string]json.RawMessage
	if err := json.Unmarshal(lockData, &updated); err != nil {
		t.Fatal(err)
	}
	var futureField map[string]bool
	if err := json.Unmarshal(updated["futureField"], &futureField); err != nil || !futureField["preserved"] {
		t.Errorf("removal lost unknown Global lock field: %s (error %v)", lockData, err)
	}
}

func TestInstallAliasesRestoreLocalAndGitSkillsFromProjectLock(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	localSkill := filepath.Join(root, "local-source", "skills", "local-alpha")
	for _, directory := range []string{project, home, localSkill} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, localSkill, "local-alpha")
	if err := os.WriteFile(filepath.Join(localSkill, "guide.md"), []byte("local support"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/git-beta/SKILL.md":          "---\nname: git-beta\ndescription: git skill\n---\n",
		"skills/git-beta/references/ref.md": "git support file",
		"unrelated/README.md":               "unrelated content",
	})
	gitSource := "file://" + filepath.ToSlash(fixture.Bare) + "#main"

	for _, test := range []struct {
		name   string
		source string
		skill  string
	}{
		{name: "local", source: filepath.Dir(filepath.Dir(localSkill)), skill: "local-alpha"},
		{name: "git", source: gitSource, skill: "git-beta"},
	} {
		stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", test.source, "--skill", test.skill, "--agent", "claude-code", "--copy", "--yes")
		if exitCode != 0 {
			t.Fatalf("add %s skill: exit %d; stdout: %s; stderr: %s", test.name, exitCode, stdout, stderr)
		}
	}
	lockBefore, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(project, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{"experimental_install", "install", "i"} {
		stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", command)
		if exitCode != 0 {
			t.Fatalf("%s restore: exit %d; stdout: %s; stderr: %s", command, exitCode, stdout, stderr)
		}
		for _, skill := range []string{"local-alpha", "git-beta"} {
			if _, err := os.Stat(filepath.Join(project, ".agents", "skills", skill, "SKILL.md")); err != nil {
				t.Errorf("%s did not restore %s: %v", command, skill, err)
			}
		}
		if content, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "local-alpha", "guide.md")); err != nil || string(content) != "local support" {
			t.Errorf("restored local support = %q, error %v", content, err)
		}
		if content, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "git-beta", "references", "ref.md")); err != nil || string(content) != "git support file" {
			t.Errorf("restored Git support = %q, error %v", content, err)
		}
		if command != "i" {
			if err := os.RemoveAll(filepath.Join(project, ".agents", "skills")); err != nil {
				t.Fatal(err)
			}
		}
	}
	lockAfter, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(lockAfter) != string(lockBefore) {
		t.Error("restore rewrote the Project lock instead of using it as the source of truth")
	}
}

func TestGlobalGitHubCheckUsesTreesMetadataAndLeavesLockUntouched(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "stable support",
		"unrelated/large.bin":            strings.Repeat("unrelated payload ", 2048),
	})
	working := filepath.Join(root, "git-work")
	gitTest(t, working, "checkout", "-qb", "feature/new")
	if err := os.WriteFile(filepath.Join(working, "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\ndescription: default branch skill\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/alpha/SKILL.md")
	gitTest(t, working, "commit", "-qm", "default branch skill")
	gitTest(t, working, "push", "-q", "origin", "HEAD:refs/heads/feature/new")
	gitTest(t, "", "--git-dir="+fixture.Bare, "symbolic-ref", "HEAD", "refs/heads/feature/new")
	var metadataRequests, treeRequests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-github-token" {
			t.Errorf("GitHub API authorization = %q, want environment token", request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.EscapedPath() {
		case "/repos/test/repo":
			metadataRequests.Add(1)
			_, _ = response.Write([]byte(`{"default_branch":"feature/new"}`))
		case "/repos/test/repo/git/trees/feature%2Fnew":
			treeRequests.Add(1)
			if request.URL.Query().Get("recursive") != "1" {
				t.Errorf("Trees API query = %s, want recursive=1", request.URL.RawQuery)
			}
			_, _ = response.Write(githubTreeResponse(t, fixture.Bare, "feature/new"))
		default:
			t.Errorf("unexpected GitHub API route %s?%s", request.URL.EscapedPath(), request.URL.RawQuery)
			http.NotFound(response, request)
		}
	}))
	defer api.Close()

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("global GitHub add: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	listOutput, listError, listExit := runCLIAt(t, binary, project, home, "", "list", "--global", "--json")
	if listExit != 0 {
		t.Fatalf("global list exit = %d; stdout: %s; stderr: %s", listExit, listOutput, listError)
	}
	var globalList []map[string]any
	if err := json.Unmarshal([]byte(listOutput), &globalList); err != nil || len(globalList) != 1 || globalList[0]["sourceType"] != "github" {
		t.Fatalf("global list metadata = %s, error %v", listOutput, err)
	}
	trace := filepath.Join(root, "check-git.trace")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{
		"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL,
		"GITHUB_TOKEN=test-github-token",
		"GIT_TRACE_PACKET=" + trace,
	}, "check")
	if exitCode != 0 {
		t.Fatalf("global metadata check: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if metadataRequests.Load() != 1 || treeRequests.Load() != 1 {
		t.Errorf("GitHub API requests = metadata:%d tree:%d, want one default-branch lookup and one tree request", metadataRequests.Load(), treeRequests.Load())
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("unchanged metadata check rewrote the Global lock")
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Errorf("successful Trees API check invoked Git; packet trace stat error = %v", err)
	}
}

func TestGlobalGitHubCheckKeepsPinnedRefWithoutDefaultBranchLookup(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: pinned branch\n---\n",
	})
	working := filepath.Join(root, "git-work")
	gitTest(t, working, "checkout", "-qb", "feature/new")
	if err := os.WriteFile(filepath.Join(working, "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\ndescription: other branch\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/alpha/SKILL.md")
	gitTest(t, working, "commit", "-qm", "other branch")
	gitTest(t, working, "push", "-q", "origin", "HEAD:refs/heads/feature/new")

	var metadataRequests, mainTreeRequests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.EscapedPath() {
		case "/repos/test/repo":
			metadataRequests.Add(1)
			_, _ = response.Write([]byte(`{"default_branch":"feature/new"}`))
		case "/repos/test/repo/git/trees/main":
			mainTreeRequests.Add(1)
			_, _ = response.Write(githubTreeResponse(t, fixture.Bare, "main"))
		default:
			t.Errorf("unexpected pinned-ref GitHub API route %s", request.URL.EscapedPath())
			http.NotFound(response, request)
		}
	}))
	defer api.Close()

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo#main", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("install pinned Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL}, "check", "--global")
	if exitCode != 0 {
		t.Fatalf("check pinned Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if metadataRequests.Load() != 0 || mainTreeRequests.Load() != 1 {
		t.Errorf("pinned-ref API requests = metadata:%d main-tree:%d, want 0 and 1", metadataRequests.Load(), mainTreeRequests.Load())
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("pinned unchanged check rewrote the Global lock")
	}
}

func githubTreeResponse(t *testing.T, bare, ref string) []byte {
	t.Helper()
	rootTree := strings.TrimSpace(gitOutput(t, "", "--git-dir="+bare, "rev-parse", "refs/heads/"+ref+"^{tree}"))
	listing := gitOutput(t, "", "--git-dir="+bare, "ls-tree", "-r", "-t", "refs/heads/"+ref)
	entries := make([]map[string]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if line == "" {
			continue
		}
		metadata, name, found := strings.Cut(line, "\t")
		fields := strings.Fields(metadata)
		if !found || len(fields) != 3 {
			t.Fatalf("parse Git tree fixture line %q", line)
		}
		entries = append(entries, map[string]string{"path": name, "type": fields[1], "sha": fields[2]})
	}
	return mustJSON(t, map[string]any{"sha": rootTree, "truncated": false, "tree": entries})
}

func gitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(output)
}

func startGitDaemon(t *testing.T, root, bare string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local Git daemon port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release local Git daemon port: %v", err)
	}
	command := exec.Command("git", "daemon", "--reuseaddr", "--export-all", "--base-path="+root, "--listen=127.0.0.1", "--port="+strconv.Itoa(port))
	if err := command.Start(); err != nil {
		t.Fatalf("start local Git daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return "git://" + address + "/" + filepath.Base(bare)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("local Git daemon did not start listening")
	return ""
}

func TestProjectCheckUsesPartialGitAndIgnoresUnrelatedRepositoryChanges(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := strings.Repeat("unrelated binary payload ", 4096)
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":            "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/guide.md": "locked support file",
		"skills/beta/SKILL.md":             "---\nname: beta\ndescription: beta description\n---\n",
		"skills/beta/references/guide.md":  "unselected skill support blob",
		"unrelated/large.bin":              unrelated,
	})
	source := startGitDaemon(t, root, fixture.Bare) + "#main"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("add project Git skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lockPath := filepath.Join(project, "skills-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "git-work")
	if err := os.WriteFile(filepath.Join(working, "unrelated", "large.bin"), []byte("unrelated file changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "unrelated/large.bin")
	gitTest(t, working, "commit", "-qm", "unrelated-only change")
	gitTest(t, working, "push", "-q", "origin", "main:main")

	trace := filepath.Join(root, "project-check.trace")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{"GIT_TRACE_PACKET=" + trace}, "check", "--project")
	if exitCode != 0 {
		t.Fatalf("project check: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if strings.Contains(stdout, "Updated project skill alpha") {
		t.Errorf("unrelated repository change triggered an update: %s", stdout)
	}
	traceData, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("partial Git packet trace missing: %v", err)
	}
	for label, contents := range map[string]string{
		"unrelated repository":     unrelated,
		"unselected skill support": "unselected skill support blob",
	} {
		if hash := gitBlobHash(t, contents); strings.Contains(string(traceData), hash) {
			t.Errorf("project check transferred %s blob", label)
		}
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("unchanged Project check rewrote skills-lock.json")
	}
}

func TestGlobalCheckResolvesMovedAndDeletedSkillsThroughPartialGit(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "version one",
	})
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write(githubTreeResponse(t, fixture.Bare, "main"))
	}))
	defer api.Close()
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("add Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	working := filepath.Join(root, "git-work")
	gitTest(t, working, "mv", "skills/alpha", "skills/renamed-alpha")
	if err := os.WriteFile(filepath.Join(working, "skills", "renamed-alpha", "references", "ref.md"), []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "--all")
	gitTest(t, working, "commit", "-qm", "move and update Global skill")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	apiURL := []string{"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL}
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", apiURL, "update", "-g", "-y")
	if exitCode != 0 {
		t.Fatalf("update moved Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if content, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "alpha", "references", "ref.md")); err != nil || string(content) != "version two" {
		t.Fatalf("moved Global skill content = %q, error %v; output=%s", content, err, stdout)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	beforeDelete, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "rm", "-r", "skills/renamed-alpha")
	gitTest(t, working, "commit", "-qm", "delete moved skill")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", apiURL, "check", "--global")
	if exitCode != 0 || !strings.Contains(stdout, "appears deleted upstream") {
		t.Fatalf("check deleted Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("check removed the deleted Global skill without confirmation: %v", err)
	}
	afterDelete, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDelete) != string(beforeDelete) {
		t.Error("noninteractive Global deletion changed the lock")
	}
}

func TestWellKnownUpdatesUseStandardIndexAndReinstallChangedSkills(t *testing.T) {
	binary := buildCLI(t)
	for _, scope := range []struct {
		name string
		flag string
		path func(project, home string) string
	}{
		{name: "global", flag: "--global", path: func(project, home string) string {
			return filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md")
		}},
		{name: "project", flag: "--project", path: func(project, home string) string {
			return filepath.Join(project, ".claude", "skills", "alpha", "SKILL.md")
		}},
	} {
		t.Run(scope.name, func(t *testing.T) {
			root := t.TempDir()
			project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
			for _, directory := range []string{project, home} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var skillContent atomic.Value
			skillContent.Store("---\nname: alpha\ndescription: initial well-known skill\n---\n\ninitial body\n")
			var indexRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/.well-known/agent-skills/index.json":
					indexRequests.Add(1)
					_, _ = response.Write([]byte(`{"skills":[{"name":"alpha","description":"indexed alpha","files":["SKILL.md"]}]}`))
				case "/.well-known/agent-skills/alpha/SKILL.md":
					response.Header().Set("Content-Type", "text/markdown")
					_, _ = response.Write([]byte(skillContent.Load().(string)))
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", server.URL, "--skill", "alpha", "--agent", "claude-code", "--copy", scope.flag, "--yes")
			if exitCode != 0 {
				t.Fatalf("add %s well-known skill: exit %d; stdout: %s; stderr: %s", scope.name, exitCode, stdout, stderr)
			}
			lockPath := filepath.Join(project, "skills-lock.json")
			if scope.name == "global" {
				lockPath = filepath.Join(home, ".agents", ".skill-lock.json")
			}
			lockData, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			var lock map[string]any
			if err := json.Unmarshal(lockData, &lock); err != nil {
				t.Fatal(err)
			}
			entries := lock["skills"].(map[string]any)
			entry := entries["alpha"].(map[string]any)
			entry["futureEntryField"] = "preserve me"
			lock["futureTopLevelField"] = map[string]any{"preserved": true}
			lockData = mustJSON(t, lock)
			if err := os.WriteFile(lockPath, lockData, 0o600); err != nil {
				t.Fatal(err)
			}
			beforeCheck, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			indexCount := indexRequests.Load()
			stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "check", scope.flag)
			if exitCode != 0 {
				t.Fatalf("check unchanged %s well-known skill: exit %d; stdout: %s; stderr: %s", scope.name, exitCode, stdout, stderr)
			}
			if indexRequests.Load() <= indexCount {
				t.Error("unchanged well-known check did not request the standard index")
			}
			afterCheck, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterCheck) != string(beforeCheck) {
				t.Error("unchanged well-known check rewrote its lock")
			}

			updatedContent := "---\nname: alpha\ndescription: updated well-known skill\n---\n\nupdated body\n"
			skillContent.Store(updatedContent)
			stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "update", scope.flag, "--yes")
			if exitCode != 0 {
				t.Fatalf("update changed %s well-known skill: exit %d; stdout: %s; stderr: %s", scope.name, exitCode, stdout, stderr)
			}
			installed, err := os.ReadFile(scope.path(project, home))
			if err != nil || string(installed) != updatedContent {
				t.Fatalf("updated %s skill content = %q, error %v", scope.name, installed, err)
			}
			updatedLock, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(updatedLock, &lock); err != nil {
				t.Fatal(err)
			}
			if lock["futureTopLevelField"].(map[string]any)["preserved"] != true {
				t.Errorf("well-known update lost unknown top-level lock field: %s", updatedLock)
			}
			updatedEntry := lock["skills"].(map[string]any)["alpha"].(map[string]any)
			if updatedEntry["futureEntryField"] != "preserve me" {
				t.Errorf("well-known update lost unknown entry lock field: %s", updatedLock)
			}
			if updatedEntry["wellKnownDigest"] == entry["wellKnownDigest"] {
				t.Errorf("well-known update left stale digest %v", updatedEntry["wellKnownDigest"])
			}
			if scope.name == "project" && updatedEntry["computedHash"] == entry["computedHash"] {
				t.Errorf("project well-known update left stale content hash %v", updatedEntry["computedHash"])
			}
		})
	}
}

func TestWellKnownV2UpdatesUseAdvertisedArtifactDigest(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var skillContent atomic.Value
	skillContent.Store("---\nname: alpha\ndescription: v2 initial skill\n---\n")
	var indexRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/agent-skills/index.json":
			indexRequests.Add(1)
			content := []byte(skillContent.Load().(string))
			digest := sha256.Sum256(content)
			index := fmt.Sprintf(`{"$schema":%q,"skills":[{"name":"alpha","description":"v2 skill","type":"skill-md","url":"/.well-known/agent-skills/alpha/SKILL.md","digest":"sha256:%s"}]}`, wellKnownSchemaV2, hex.EncodeToString(digest[:]))
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(index))
		case "/.well-known/agent-skills/alpha/SKILL.md":
			_, _ = response.Write([]byte(skillContent.Load().(string)))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", server.URL, "--skill", "alpha", "--agent", "claude-code", "--copy", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("add v2 well-known skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lockDocument struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(before, &lockDocument); err != nil {
		t.Fatal(err)
	}
	initialDigest := lockDocument.Skills["alpha"]["wellKnownDigest"]
	content := []byte(skillContent.Load().(string))
	initialHash := sha256.Sum256(content)
	if initialDigest != "sha256:"+hex.EncodeToString(initialHash[:]) {
		t.Fatalf("v2 lock digest = %v, want the advertised artifact digest", initialDigest)
	}

	indexCount := indexRequests.Load()
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "check", "--global")
	if exitCode != 0 {
		t.Fatalf("check unchanged v2 well-known skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if indexRequests.Load() <= indexCount {
		t.Error("unchanged v2 check did not read the standard index")
	}
	afterCheck, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterCheck) != string(before) {
		t.Error("unchanged v2 well-known check rewrote the Global lock")
	}

	updatedContent := "---\nname: alpha\ndescription: v2 updated skill\n---\n"
	skillContent.Store(updatedContent)
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "update", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("update changed v2 well-known skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	installed, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "alpha", "SKILL.md"))
	if err != nil || string(installed) != updatedContent {
		t.Fatalf("updated v2 skill content = %q, error %v", installed, err)
	}
	updatedLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(updatedLock, &lockDocument); err != nil {
		t.Fatal(err)
	}
	updatedDigest := lockDocument.Skills["alpha"]["wellKnownDigest"]
	updatedHash := sha256.Sum256([]byte(updatedContent))
	if updatedDigest != "sha256:"+hex.EncodeToString(updatedHash[:]) || updatedDigest == initialDigest {
		t.Errorf("updated v2 digest = %v, want the new advertised artifact digest", updatedDigest)
	}
}

func TestGitHubEnterpriseLockUsesGitWithoutCallingGitHubDotComAPI(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: alpha description\n---\n",
	})
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("add global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	config := filepath.Join(home, ".gitconfig")
	file, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintf(file, "\n[url \"file://%s\"]\n\tinsteadOf = https://github.enterprise.invalid/test/repo.git\n", filepath.ToSlash(fixture.Bare))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("add enterprise Git URL rewrite: write %v, close %v", writeErr, closeErr)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(lockData, &document); err != nil {
		t.Fatal(err)
	}
	skills := document["skills"].(map[string]any)
	skill := skills["alpha"].(map[string]any)
	skill["sourceUrl"] = "https://github.enterprise.invalid/test/repo.git"
	if err := os.WriteFile(lockPath, mustJSON(t, document), 0o600); err != nil {
		t.Fatal(err)
	}
	var apiRequests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		apiRequests.Add(1)
		http.Error(response, "GitHub.com API is not for Enterprise Server", http.StatusInternalServerError)
	}))
	defer api.Close()
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL}, "check", "--global")
	if exitCode != 0 {
		t.Fatalf("Enterprise fallback check: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if apiRequests.Load() != 0 {
		t.Errorf("GitHub Enterprise source called public GitHub API %d times", apiRequests.Load())
	}
	if !strings.Contains(stderr, "GitHub Trees API unavailable") {
		t.Errorf("Enterprise check did not report Git fallback: %s", stderr)
	}
}

func TestGlobalGenericGitUsesContentHashAndDoesNotCallGitHubAPI(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "version one",
	})
	source := startGitDaemon(t, root, fixture.Bare) + "#main"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("add generic Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	var apiRequests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		apiRequests.Add(1)
		http.Error(response, "must not query github for generic Git source", http.StatusInternalServerError)
	}))
	defer api.Close()
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var oldDocument struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(before, &oldDocument); err != nil {
		t.Fatal(err)
	}
	oldHash, ok := oldDocument.Skills["alpha"]["skillFolderHash"].(string)
	if !ok || len(oldHash) != 64 {
		t.Fatalf("generic Global lock hash = %#v, want 64-character content hash", oldDocument.Skills["alpha"]["skillFolderHash"])
	}
	working := filepath.Join(root, "git-work")
	if err := os.WriteFile(filepath.Join(working, "skills", "alpha", "references", "ref.md"), []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/alpha/references/ref.md")
	gitTest(t, working, "commit", "-qm", "change generic Global skill")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL}, "update", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("update generic Global skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if apiRequests.Load() != 0 {
		t.Errorf("generic Global source made %d GitHub API requests", apiRequests.Load())
	}
	if content, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "alpha", "references", "ref.md")); err != nil || string(content) != "version two" {
		t.Fatalf("generic Global skill content = %q, error %v", content, err)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &oldDocument); err != nil {
		t.Fatal(err)
	}
	newHash, _ := oldDocument.Skills["alpha"]["skillFolderHash"].(string)
	if newHash == oldHash || len(newHash) != 64 {
		t.Errorf("generic Global lock content hash = %q, was %q", newHash, oldHash)
	}
}

func TestGlobalCheckFallsBackToPartialGitWhenTreesAPIIsUnavailable(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "stable support",
		"unrelated/large.bin":            strings.Repeat("unrelated payload ", 2048),
	})
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("global add: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	gitTest(t, "", "--git-dir="+fixture.Bare, "config", "uploadpack.allowFilter", "false")
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "offline fixture", http.StatusServiceUnavailable)
	}))
	defer api.Close()
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "global-fallback.trace")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{
		"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL,
		"GIT_TRACE_PACKET=" + trace,
	}, "upgrade", "-g")
	if exitCode != 0 {
		t.Fatalf("Git fallback check: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stderr, "Trees API unavailable") || !strings.Contains(strings.ToLower(stderr), "filter") {
		t.Errorf("fallback/filter capability was not explained: %s", stderr)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("unchanged fallback check rewrote the Global lock")
	}
}

func TestProjectUpdateReinstallsMovedSkillAndKeepsDeletedSkillAndLock(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "version one",
		"unrelated/large.bin":            strings.Repeat("other source ", 1024),
	})
	source := "file://" + filepath.ToSlash(fixture.Bare) + "#main"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("add source skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	working := filepath.Join(root, "git-work")
	gitTest(t, working, "mv", "skills/alpha", "skills/renamed-alpha")
	if err := os.WriteFile(filepath.Join(working, "skills", "renamed-alpha", "references", "ref.md"), []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "--all")
	gitTest(t, working, "commit", "-qm", "move and update alpha")
	gitTest(t, working, "push", "-q", "origin", "main:main")

	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "update", "--project", "--yes", "alpha")
	if exitCode != 0 {
		t.Fatalf("update moved skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	installed := filepath.Join(project, ".claude", "skills", "alpha")
	if content, err := os.ReadFile(filepath.Join(installed, "references", "ref.md")); err != nil || string(content) != "version two" {
		t.Fatalf("moved skill was not reinstalled: content=%q error=%v; output=%s", content, err, stdout)
	}
	lockPath := filepath.Join(project, "skills-lock.json")
	lock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(lock, &document); err != nil {
		t.Fatal(err)
	}
	if path := document.Skills["alpha"]["skillPath"]; path != "skills/renamed-alpha/SKILL.md" {
		t.Errorf("updated lock skillPath = %#v, want relocated path", path)
	}
	if _, ok := document.Skills["alpha"]["computedHash"].(string); !ok {
		t.Errorf("updated lock does not retain Project content hash: %#v", document.Skills["alpha"])
	}

	gitTest(t, working, "rm", "-r", "skills/renamed-alpha")
	gitTest(t, working, "commit", "-qm", "delete alpha")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	beforeDeleteCheck, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "check", "-p", "-y")
	if exitCode != 0 {
		t.Fatalf("check deleted skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "appears deleted upstream") {
		t.Errorf("deleted skill was not reported as a skipped deletion: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); err != nil {
		t.Errorf("non-interactive deletion removed local skill: %v", err)
	}
	afterDeleteCheck, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDeleteCheck) != string(beforeDeleteCheck) {
		t.Error("non-interactive deletion changed the Project lock")
	}
}

func TestGlobalUpdateAppliesChangedSkillPathFromTreesMetadata(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := strings.Repeat("unrelated object ", 3072)
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "version one",
		"unrelated/large.bin":            unrelated,
	})
	var requestCount atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(githubTreeResponse(t, fixture.Bare, "main"))
	}))
	defer api.Close()
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", "test/repo", "--skill", "alpha", "--agent", "claude-code", "--global", "--yes")
	if exitCode != 0 {
		t.Fatalf("add global GitHub skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	working := filepath.Join(root, "git-work")
	if err := os.WriteFile(filepath.Join(working, "skills", "alpha", "references", "ref.md"), []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/alpha/references/ref.md")
	gitTest(t, working, "commit", "-qm", "update alpha only")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	trace := filepath.Join(root, "global-update.trace")
	stdout, stderr, exitCode = runCLIAtWithEnv(t, binary, project, home, "", []string{
		"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL,
		"GIT_TRACE_PACKET=" + trace,
	}, "upgrade", "-g")
	if exitCode != 0 {
		t.Fatalf("global update: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if requestCount.Load() != 1 {
		t.Errorf("Trees API request count = %d, want one", requestCount.Load())
	}
	installed := filepath.Join(home, ".claude", "skills", "alpha")
	if content, err := os.ReadFile(filepath.Join(installed, "references", "ref.md")); err != nil || string(content) != "version two" {
		t.Fatalf("global skill was not updated: content=%q error=%v", content, err)
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var global struct {
		Skills map[string]map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(lockData, &global); err != nil {
		t.Fatal(err)
	}
	expectedTree := strings.TrimSpace(gitOutput(t, "", "--git-dir="+fixture.Bare, "rev-parse", "refs/heads/main:skills/alpha"))
	if got := global.Skills["alpha"]["skillFolderHash"]; got != expectedTree {
		t.Errorf("updated Global tree hash = %#v, want %s", got, expectedTree)
	}
	traceData, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("Git packet trace missing during changed-skill materialization: %v", err)
	}
	if hash := gitBlobHash(t, unrelated); strings.Contains(string(traceData), hash) {
		t.Error("global update fetched an unrelated repository blob")
	}
}

func TestRemoveWithoutConfirmationSkipsInNoninteractiveMode(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	source := filepath.Join(root, "source", "alpha")
	for _, directory := range []string{project, home, source} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, source, "alpha")
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("add skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "remove", "alpha")
	if exitCode != 0 {
		t.Fatalf("unconfirmed noninteractive remove exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "skipped") {
		t.Errorf("remove did not explain noninteractive skip: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("unconfirmed remove deleted the skill: %v", err)
	}
}

func TestGenericGitUpdateSkipsAmbiguousDuplicateSkillNames(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: original skill\n---\n",
	})
	source := "file://" + filepath.ToSlash(fixture.Bare) + "#main"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "claude-code", "--copy", "--yes")
	if exitCode != 0 {
		t.Fatalf("add Git skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	working := filepath.Join(root, "git-work")
	duplicate := filepath.Join(working, "skills", "another")
	if err := os.MkdirAll(duplicate, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(duplicate, "SKILL.md"), []byte("---\nname: alpha\ndescription: ambiguous duplicate\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/another/SKILL.md")
	gitTest(t, working, "commit", "-qm", "introduce duplicate skill name")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	lockPath := filepath.Join(project, "skills-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "update", "-p", "-y")
	if exitCode != 0 {
		t.Fatalf("ambiguous update exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stderr, "multiple current paths match alpha") {
		t.Errorf("ambiguous source was not reported and skipped: %s", stderr)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("ambiguous generic Git source changed the lock")
	}
}

func TestUpdateFailureCodesAndDefaultScopeRemainReadOnly(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	projectLock := map[string]any{"version": 1, "skills": map[string]any{
		"local-only": map[string]any{"source": filepath.Join(root, "local-source"), "sourceType": "local", "computedHash": "local-hash"},
	}}
	projectLockPath := filepath.Join(project, "skills-lock.json")
	if err := os.WriteFile(projectLockPath, mustJSON(t, projectLock), 0o600); err != nil {
		t.Fatal(err)
	}
	globalLockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if err := os.MkdirAll(filepath.Dir(globalLockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	globalLock := []byte(`{"version":3,"skills":{"broken":{"source":"test/repo","sourceUrl":"file:///missing/repository.git","sourceType":"github","skillPath":"skills/alpha/SKILL.md","skillFolderHash":"0123456789012345678901234567890123456789"}}}`)
	if err := os.WriteFile(globalLockPath, globalLock, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"check"}, {"update", "--yes"}} {
		stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", args...)
		if exitCode != 0 {
			t.Fatalf("default scope command %v selected Global unexpectedly: exit %d; stdout: %s; stderr: %s", args, exitCode, stdout, stderr)
		}
	}
	globalAfter, err := os.ReadFile(globalLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(globalAfter) != string(globalLock) {
		t.Error("default project-scoped checks touched the Global lock")
	}

	brokenProjectLock := []byte(`{"version":1,"skills":{"broken":{"source":"https://invalid.local/repository.git","sourceUrl":"file:///missing/repository.git","sourceType":"git","ref":"main","skillPath":"skills/alpha/SKILL.md","computedHash":"old-hash"}}}`)
	if err := os.WriteFile(projectLockPath, brokenProjectLock, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "check", "-p")
	if exitCode != 1 {
		t.Fatalf("failed source check exit = %d, want 1; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	projectAfter, err := os.ReadFile(projectLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(projectAfter) != string(brokenProjectLock) {
		t.Error("failed read-only check rewrote the Project lock")
	}
}

func TestLegacyGlobalLockIsSkippedWithoutRewrite(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"version":1,"skills":{"legacy":{"source":"test/repo","sourceType":"github","sourceUrl":"file:///missing/repository.git","contentHash":"old-hash"}}}`)
	if err := os.WriteFile(lockPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "check")
	if exitCode != 0 || !strings.Contains(stdout, "no supported version metadata") {
		t.Fatalf("legacy Global check = exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(legacy) {
		t.Error("read-only legacy Global check rewrote the lock")
	}
}

func TestProjectUpdatePreservesEveSubagentDestinations(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, directory := range []string{project, home} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := createGitHubFixture(t, root, home, map[string]string{
		"skills/alpha/SKILL.md":          "---\nname: alpha\ndescription: alpha description\n---\n",
		"skills/alpha/references/ref.md": "version one",
	})
	source := "file://" + filepath.ToSlash(fixture.Bare) + "#main"
	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "add", source, "--skill", "alpha", "--agent", "eve", "--subagent", "root", "reviewer", "--yes")
	if exitCode != 0 {
		t.Fatalf("add Eve skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	working := filepath.Join(root, "git-work")
	if err := os.WriteFile(filepath.Join(working, "skills", "alpha", "references", "ref.md"), []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, working, "add", "skills/alpha/references/ref.md")
	gitTest(t, working, "commit", "-qm", "update Eve skill")
	gitTest(t, working, "push", "-q", "origin", "main:main")
	stdout, stderr, exitCode = runCLIAt(t, binary, project, home, "", "update", "-p", "-y")
	if exitCode != 0 {
		t.Fatalf("update Eve skill: exit %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	for _, directory := range []string{
		filepath.Join(project, "agent", "skills", "alpha"),
		filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha"),
	} {
		if content, err := os.ReadFile(filepath.Join(directory, "references", "ref.md")); err != nil || string(content) != "version two" {
			t.Errorf("updated Eve target %s contains %q, error %v", directory, content, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("Eve update created an unrelated canonical installation: %v", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestGlobalListTextUsesReadableRowsAndHomeShortcut(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	skillDir := filepath.Join(home, ".agents", "skills", "alpha")
	for _, directory := range []string{project, skillDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeTestSkill(t, skillDir, "alpha")

	stdout, stderr, exitCode := runCLIAt(t, binary, project, home, "", "ls", "-g")
	if exitCode != 0 {
		t.Fatalf("global list exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) < 4 || lines[0] != "Global Skills" {
		t.Fatalf("global list layout = %q, want a heading and two lines per skill", stdout)
	}
	if !strings.Contains(stdout, "alpha") || !strings.Contains(stdout, "~/.agents/skills/alpha") {
		t.Fatalf("global list path was not shortened: %q", stdout)
	}
	if strings.Contains(stdout, home) || strings.Contains(stdout, "\t") {
		t.Fatalf("global list contains an absolute home path or tab-separated columns: %q", stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("captured non-terminal output unexpectedly contains ANSI color: %q", stdout)
	}
}

func TestListTextUsesColorsWhenTerminalColorsAreEnabled(t *testing.T) {
	source := "owner/repo"
	var output strings.Builder
	writeListText(&output, []listResult{{
		Name: "alpha", Path: filepath.Join(os.TempDir(), "project", ".agents", "skills", "alpha"),
		Agents: []string{"Codex"}, Source: &source,
	}}, true, filepath.Join(os.TempDir(), "project"), true)

	for _, color := range []string{"\x1b[1mGlobal Skills\x1b[0m", "\x1b[36malpha\x1b[0m", "\x1b[38;5;102mAgents:\x1b[0m"} {
		if !strings.Contains(output.String(), color) {
			t.Errorf("colored list output %q does not contain %q", output.String(), color)
		}
	}
}

func TestGlobalUpdateFilterAcceptsRepositorySource(t *testing.T) {
	binary := buildCLI(t)
	root := t.TempDir()
	project, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	globalLockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(globalLockPath), 0o700); err != nil {
		t.Fatal(err)
	}

	const (
		wayfinderHash = "1111111111111111111111111111111111111111"
		wizardHash    = "2222222222222222222222222222222222222222"
		writingHash   = "3333333333333333333333333333333333333333"
	)
	entry := func(skillPath, folderHash string) map[string]string {
		return map[string]string{
			"source": "mattpocock/skills", "sourceType": "github",
			"sourceUrl": "https://github.com/mattpocock/skills.git",
			"skillPath": skillPath + "/SKILL.md", "skillFolderHash": folderHash,
		}
	}
	lock := map[string]any{"version": 3, "skills": map[string]any{
		"wayfinder":          entry("skills/wayfinder", wayfinderHash),
		"wizard":             entry("skills/wizard", wizardHash),
		"writing-for-agents": entry("skills/writing-for-agents", writingHash),
		"unrelated": map[string]string{
			"source": "other/repo", "sourceType": "github",
			"sourceUrl": "https://github.com/other/repo.git",
			"skillPath": "skills/unrelated/SKILL.md", "skillFolderHash": "4444444444444444444444444444444444444444",
		},
	}}
	lockBytes, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalLockPath, lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	var apiRequests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		apiRequests.Add(1)
		switch request.URL.Path {
		case "/repos/mattpocock/skills":
			fmt.Fprint(response, `{"default_branch":"main"}`)
		case "/repos/mattpocock/skills/git/trees/main":
			fmt.Fprintf(response, `{"sha":"root","tree":[{"path":"skills/wayfinder","type":"tree","sha":"%s"},{"path":"skills/wayfinder/SKILL.md","type":"blob","sha":"a"},{"path":"skills/wizard","type":"tree","sha":"%s"},{"path":"skills/wizard/SKILL.md","type":"blob","sha":"b"},{"path":"skills/writing-for-agents","type":"tree","sha":"%s"},{"path":"skills/writing-for-agents/SKILL.md","type":"blob","sha":"c"}]}`, wayfinderHash, wizardHash, writingHash)
		default:
			http.NotFound(response, request)
		}
	}))
	defer api.Close()

	stdout, stderr, exitCode := runCLIAtWithEnv(t, binary, project, home, "", []string{"SKILLS_GO_TEST_GITHUB_API_URL=" + api.URL}, "update", "-g", "mattpocock/skills")
	if exitCode != 0 {
		t.Fatalf("source-filtered global update exit = %d; stdout: %s; stderr: %s", exitCode, stdout, stderr)
	}
	if strings.Contains(stdout, "No installed skills found matching") {
		t.Fatalf("repository source filter did not match its lock entries: %s", stdout)
	}
	if apiRequests.Load() != 2 {
		t.Fatalf("source-filtered update made %d GitHub API requests, want one repository and one tree request", apiRequests.Load())
	}
	lockAfter, err := os.ReadFile(globalLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(lockAfter) != string(lockBytes) {
		t.Fatalf("unchanged source filter rewrote the global lock: %s", lockAfter)
	}
}
