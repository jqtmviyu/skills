package skillscan

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGitMaterializeRejectsNewlinePathWithoutFetchingUnrelatedBlob(t *testing.T) {
	const skillPath = "skills/unsafe\nunrelated"
	fixture := newGitFixtureWithFiles(t, true, false, map[string]string{
		skillPath + "/SKILL.md": "---\nname: unsafe\ndescription: Newline path.\n---\nSkill body.\n",
	})
	scan, err := Open(context.Background(), fixture.URL, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open Git source: %v", err)
	}
	defer scan.Close()

	if !scan.FilterStatus().Available {
		t.Fatalf("fixture does not support partial filtering: %#v", scan.FilterStatus())
	}
	if !contains(candidatePaths(scan.Candidates()), skillPath) {
		t.Fatalf("discovery did not retain the newline skill path %q: %v", skillPath, candidatePaths(scan.Candidates()))
	}
	unrelatedBlob := scan.fileObjects["unrelated/large.bin"]
	if objectExistsWithoutLazyFetch(t, scan.root, unrelatedBlob) {
		t.Fatal("unrelated large blob was fetched before materialization")
	}

	_, err = scan.Materialize([]string{skillPath})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "line break") {
		t.Errorf("Materialize error = %v, want a clear line-break path error", err)
	}
	if got := worktreeFiles(t, scan.root); len(got) != 0 {
		t.Fatalf("failed materialization checked out files: %v", got)
	}
	if objectExistsWithoutLazyFetch(t, scan.root, unrelatedBlob) {
		t.Fatal("failed materialization fetched the unrelated large blob")
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestGitUpdateDiscoveryBatchesMetadataBlobFetches(t *testing.T) {
	fixture := newGitFixture(t, true, false)
	trace := filepath.Join(t.TempDir(), "git-packets.log")
	t.Setenv("GIT_TRACE_PACKET", trace)

	var progress bytes.Buffer
	scan, err := Open(context.Background(), fixture.URL+"#main", Options{Progress: &progress})
	if err != nil {
		t.Fatalf("Open Git source: %v", err)
	}
	defer scan.Close()
	if len(scan.Candidates()) != 3 {
		t.Fatalf("discovered skills = %d, want 3", len(scan.Candidates()))
	}
	if !strings.Contains(progress.String(), "in one Git request") {
		t.Fatalf("metadata batch progress was not reported: %s", progress.String())
	}
	packets, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	fetchCommands := strings.Count(string(packets), "command=fetch")
	if fetchCommands != 2 {
		t.Fatalf("Git fetch RPC count = %d, want one commit/tree fetch plus one batched metadata fetch; progress: %s", fetchCommands, progress.String())
	}
}

func TestGitHTTPSUpdatePrefetchesMetadataInParallel(t *testing.T) {
	fixture := newGitFixture(t, true, false)
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	config := fmt.Sprintf("[url %q]\n\tinsteadOf = https://github.com/test/repo.git\n", fixture.URL)
	if err := os.WriteFile(gitConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	var progress bytes.Buffer
	scan, err := Open(context.Background(), "https://github.com/test/repo.git#main", Options{Progress: &progress})
	if err != nil {
		t.Fatalf("Open HTTPS Git source: %v", err)
	}
	defer scan.Close()
	if len(scan.Candidates()) != 3 {
		t.Fatalf("discovered skills = %d, want 3", len(scan.Candidates()))
	}
	if !strings.Contains(progress.String(), "at most 2 parallel Git HTTPS requests") {
		t.Fatalf("HTTPS metadata fetch was not batched concurrently: %s", progress.String())
	}
	if strings.Contains(progress.String(), "could not batch-fetch") {
		t.Fatalf("HTTPS metadata fetch used unsupported fetch-pack transport: %s", progress.String())
	}
}

func TestGitPartialScanFetchesMetadataThenOnlySelectedSkill(t *testing.T) {
	fixture := newGitFixture(t, true, false)
	temporaryRoot := t.TempDir()
	scan, err := Open(context.Background(), fixture.URL+"#main", Options{TempDir: temporaryRoot})
	if err != nil {
		t.Fatalf("Open Git source: %v", err)
	}
	defer scan.Close()

	if scan.Source().Ref != "main" {
		t.Fatalf("selected Git ref = %q, want main", scan.Source().Ref)
	}
	status := scan.FilterStatus()
	if !status.Requested || !status.Available || status.Fallback || status.Message != "" {
		t.Fatalf("filter status = %#v, want successful partial filtering", status)
	}
	if got, want := candidatePaths(scan.Candidates()), []string{"skills/alpha", "skills/beta", "plugins/docs/guide"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate paths = %v, want %v", got, want)
	}
	if scan.Candidates()[2].PluginName != "docs-plugin" {
		t.Fatalf("plugin candidate grouping = %q, want docs-plugin", scan.Candidates()[2].PluginName)
	}
	if _, err := os.Stat(filepath.Join(scan.root, ".git", "shallow")); err != nil {
		t.Fatalf("partial repository is not shallow: %v", err)
	}
	if got := worktreeFiles(t, scan.root); len(got) != 0 {
		t.Fatalf("initial checkout materialized files before selection: %v", got)
	}

	for _, repositoryPath := range []string{"skills/alpha/SKILL.md", "skills/beta/SKILL.md", "plugins/docs/guide/SKILL.md", ".claude/skills/locked/SKILL.md", "skills-lock.json", ".claude-plugin/marketplace.json"} {
		if scan.fileObjects[repositoryPath] == "" {
			t.Fatalf("Git tree is missing expected metadata path %q; indexed paths=%v", repositoryPath, scan.paths)
		}
		if !objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects[repositoryPath]) {
			t.Errorf("metadata blob %q was not fetched during discovery", repositoryPath)
		}
	}
	for _, repositoryPath := range []string{"README.md", "unrelated/large.bin", "skills/alpha/references/guide.md", "skills/alpha/scripts/run.sh", "plugins/docs/guide/support.txt"} {
		if objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects[repositoryPath]) {
			t.Errorf("payload blob %q was fetched before selection", repositoryPath)
		}
	}

	materialized, err := scan.Materialize([]string{"skills/alpha", "plugins/docs/guide"})
	if err != nil {
		t.Fatalf("materialize selected skills: %v", err)
	}
	if len(materialized) != 2 || materialized[0].Candidate.Path != "skills/alpha" || materialized[1].Candidate.Path != "plugins/docs/guide" {
		t.Fatalf("materialized result = %#v", materialized)
	}
	if got, want := worktreeFiles(t, scan.root), []string{"plugins/docs/guide/SKILL.md", "plugins/docs/guide/support.txt", "skills/alpha/SKILL.md", "skills/alpha/references/guide.md", "skills/alpha/scripts/run.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized files = %v, want selected skills and support files only", got)
	}
	for _, selectedPath := range []string{"skills/alpha/references/guide.md", "skills/alpha/scripts/run.sh", "plugins/docs/guide/support.txt"} {
		if !objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects[selectedPath]) {
			t.Errorf("selected skill support blob %q was not fetched", selectedPath)
		}
	}
	for _, repositoryPath := range []string{"README.md", "unrelated/large.bin"} {
		if objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects[repositoryPath]) {
			t.Errorf("unselected blob %q was fetched after materialization", repositoryPath)
		}
	}

	scanDir := scan.root
	if err := scan.Close(); err != nil {
		t.Fatalf("close Git scan: %v", err)
	}
	if _, err := os.Stat(scanDir); !os.IsNotExist(err) {
		t.Fatalf("temporary scan directory still exists after Close: %v", err)
	}
}

func TestGitFilterRejectionFallsBackAndRemainsUsable(t *testing.T) {
	fixture := newGitFixture(t, false, false)
	scan, err := Open(context.Background(), fixture.URL, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open unfiltered Git source: %v", err)
	}
	defer scan.Close()

	status := scan.FilterStatus()
	if !status.Requested || status.Available || !status.Fallback || status.Message == "" {
		t.Fatalf("filter status = %#v, want observable full-fetch fallback", status)
	}
	for _, repositoryPath := range []string{"README.md", "unrelated/large.bin", "skills/alpha/references/guide.md"} {
		if !objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects[repositoryPath]) {
			t.Errorf("fallback did not retain complete shallow source blob %q", repositoryPath)
		}
	}
	materialized, err := scan.Materialize([]string{"skills/alpha"})
	if err != nil {
		t.Fatalf("materialize from fallback clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(materialized[0].Directory, "references/guide.md")); err != nil {
		t.Fatalf("fallback result is not installable: %v", err)
	}
}

func TestGitFilterRejectionRetriesWithoutFilterAndReportsFallback(t *testing.T) {
	fixture := newGitFixture(t, true, false, true)
	scan, err := Open(context.Background(), fixture.URL, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open filter-rejecting Git source: %v", err)
	}
	defer scan.Close()

	status := scan.FilterStatus()
	if !status.Requested || status.Available || !status.Fallback || !strings.Contains(status.Message, "rejected") {
		t.Fatalf("filter status = %#v, want reported rejection fallback", status)
	}
	if !objectExistsWithoutLazyFetch(t, scan.root, scan.fileObjects["unrelated/large.bin"]) {
		t.Fatal("filter rejection fallback did not fetch the complete shallow source")
	}
	selected, err := scan.Materialize([]string{"skills/alpha"})
	if err != nil {
		t.Fatalf("materialize after rejected filter: %v", err)
	}
	if _, err := os.Stat(filepath.Join(selected[0].Directory, "references/guide.md")); err != nil {
		t.Fatalf("rejected-filter fallback is not installable: %v", err)
	}
}

func TestGitSubpathDiscoveryKeepsRepositoryPathsAndPluginGrouping(t *testing.T) {
	fixture := newGitFixture(t, true, false)
	ctx := context.Background()
	source := Source{Kind: GitSource, URL: fixture.URL, Subpath: "plugins"}
	scan := &Scan{
		ctx: ctx, source: source, options: Options{TempDir: t.TempDir()},
		indexedFiles: make(map[string]bool), directoryChildren: make(map[string][]string),
		fileContents: make(map[string][]byte), fileObjects: make(map[string]string), byPath: make(map[string]Candidate),
	}
	if err := scan.openGit(ctx); err != nil {
		t.Fatalf("open Git subpath: %v", err)
	}
	t.Cleanup(func() { _ = scan.Close() })
	if err := scan.discover(); err != nil {
		t.Fatalf("discover Git subpath: %v", err)
	}
	if got, want := candidatePaths(scan.Candidates()), []string{"plugins/docs/guide"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subpath candidates = %v, want repository-relative path %v", got, want)
	}
	if got := scan.Candidates()[0].PluginName; got != "docs-plugin" {
		t.Fatalf("subpath plugin grouping = %q, want docs-plugin", got)
	}
	selected, err := scan.Materialize([]string{"plugins/docs/guide"})
	if err != nil {
		t.Fatalf("materialize subpath candidate: %v", err)
	}
	if selected[0].Directory != filepath.Join(scan.root, "plugins/docs/guide") {
		t.Fatalf("subpath materialized directory = %q", selected[0].Directory)
	}
	if _, err := os.Stat(filepath.Join(selected[0].Directory, "support.txt")); err != nil {
		t.Fatalf("subpath support files are unavailable: %v", err)
	}
}

func TestGitRootSkillMaterializesTheEntireRepository(t *testing.T) {
	fixture := newGitFixture(t, true, true)
	scan, err := Open(context.Background(), fixture.URL, Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open root-skill Git source: %v", err)
	}
	defer scan.Close()
	if got, want := candidatePaths(scan.Candidates()), []string{"."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root-skill candidates = %v, want root skill precedence", got)
	}
	selected, err := scan.Materialize([]string{"."})
	if err != nil {
		t.Fatalf("materialize repository-root skill: %v", err)
	}
	if selected[0].Directory != scan.root {
		t.Fatalf("root skill directory = %q, want repository root %q", selected[0].Directory, scan.root)
	}
	for _, name := range []string{"SKILL.md", "README.md", "skills/alpha/SKILL.md", "unrelated/large.bin"} {
		if _, err := os.Stat(filepath.Join(scan.root, filepath.FromSlash(name))); err != nil {
			t.Errorf("repository-root skill omitted repository file %q: %v", name, err)
		}
	}
}

type gitFixture struct {
	URL string
}

func newGitFixture(t *testing.T, filter, rootSkill bool, rejectFilter ...bool) gitFixture {
	return newGitFixtureWithFiles(t, filter, rootSkill, nil, rejectFilter...)
}

func newGitFixtureWithFiles(t *testing.T, filter, rootSkill bool, extraFiles map[string]string, rejectFilter ...bool) gitFixture {
	t.Helper()
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	bare := filepath.Join(base, "repository.git")
	runFixtureGit(t, "", "init", "--quiet", "--initial-branch=main", seed)
	runFixtureGit(t, seed, "config", "user.email", "fixture@example.invalid")
	runFixtureGit(t, seed, "config", "user.name", "Fixture")
	runFixtureGit(t, seed, "config", "core.excludesFile", "/dev/null")

	files := map[string]string{
		"README.md":                               "unrelated repository root file\n",
		"skills/alpha/SKILL.md":                   "---\nname: alpha\ndescription: Alpha skill.\n---\nAlpha body.\n",
		"skills/alpha/references/guide.md":        "Alpha support file.\n",
		"skills/beta/SKILL.md":                    "---\nname: beta\ndescription: Beta skill.\n---\nBeta body.\n",
		"plugins/docs/guide/SKILL.md":             "---\nname: docs guide\ndescription: Docs plugin skill.\n---\nPlugin body.\n",
		"plugins/docs/guide/support.txt":          "Plugin support file.\n",
		".claude-plugin/marketplace.json":         `{"metadata":{"pluginRoot":"./plugins"},"plugins":[{"name":"docs-plugin","source":"./docs","skills":["./guide"]}]}`,
		"plugins/.claude-plugin/marketplace.json": `{"plugins":[{"name":"docs-plugin","source":"./docs","skills":["./guide"]}]}`,
		".claude/skills/locked/SKILL.md":          "---\nname: locked-skill\ndescription: Already installed project skill.\n---\nLocked body.\n",
		"skills-lock.json":                        `{"version":1,"skills":{"locked-skill":{"source":"source","sourceType":"github"}}}`,
		"skills/alpha/scripts/run.sh":             "#!/bin/sh\nprintf alpha\\n\n",
		"unrelated/large.bin":                     randomPayload(512 * 1024),
	}
	for name, content := range extraFiles {
		files[name] = content
	}
	if rootSkill {
		files["SKILL.md"] = "---\nname: repository\ndescription: Repository root skill.\n---\nRoot body.\n"
	}
	for name, content := range files {
		fullPath := filepath.Join(seed, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create fixture parent: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture file %q: %v", name, err)
		}
	}
	runFixtureGit(t, seed, "add", "--all")
	runFixtureGit(t, seed, "commit", "--quiet", "-m", "fixture")
	runFixtureGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	runFixtureGit(t, seed, "push", "--quiet", bare, "HEAD:main")
	runFixtureGit(t, "", "--git-dir", bare, "config", "uploadpack.allowFilter", strconv.FormatBool(filter))
	runFixtureGit(t, "", "--git-dir", bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	if len(rejectFilter) > 0 && rejectFilter[0] {
		runFixtureGit(t, "", "--git-dir", bare, "config", "uploadpack.allowFilter", "true")
		runFixtureGit(t, "", "--git-dir", bare, "config", "uploadpackfilter.allow", "false")
	}

	port := freeTCPPort(t)
	daemon := exec.Command("git", "daemon", "--reuseaddr", "--export-all", "--base-path="+base, "--listen=127.0.0.1", "--port="+strconv.Itoa(port), "--verbose")
	logPath := filepath.Join(base, "git-daemon.log")
	daemonLog, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create local Git daemon log: %v", err)
	}
	daemon.Stdout = daemonLog
	daemon.Stderr = daemonLog
	if err := daemon.Start(); err != nil {
		_ = daemonLog.Close()
		t.Fatalf("start local Git daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = daemon.Process.Kill()
		_ = daemon.Wait()
		_ = daemonLog.Close()
	})
	endpoint := fmt.Sprintf("git://127.0.0.1:%d/repository.git", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return gitFixture{URL: endpoint}
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs, _ := os.ReadFile(logPath)
	t.Fatalf("Git daemon did not listen at %s: %s", endpoint, logs)
	return gitFixture{}
}

func runFixtureGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local Git daemon port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release local Git daemon port: %v", err)
	}
	return port
}

func randomPayload(size int) string {
	payload := make([]byte, size)
	if _, err := rand.New(rand.NewSource(29)).Read(payload); err != nil {
		panic(err)
	}
	return string(payload)
}

func objectExistsWithoutLazyFetch(t *testing.T, repository, object string) bool {
	t.Helper()
	if object == "" {
		t.Fatal("fixture did not index expected Git tree path")
	}
	command := exec.Command("git", "-C", repository, "cat-file", "-e", object)
	command.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	if err := command.Run(); err != nil {
		return false
	}
	return true
}

func worktreeFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, fullPath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() && relative == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list materialized files: %v", err)
	}
	sort.Strings(files)
	return files
}
