package lockfile_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/vercel-labs/skills/go/lockfile"
)

func TestReadGlobalSupportsHistoricalVersionsWithoutRewriting(t *testing.T) {
	fixtures := []struct {
		version   int
		entry     string
		field     string
		fieldWant string
	}{
		{version: 1, entry: `"sourceUrl":"https://example.com/acme/repo.git","installedAt":"2020-01-01T00:00:00.000Z","updatedAt":"2020-01-01T00:00:00.000Z"`},
		{version: 2, entry: `"sourceUrl":"https://example.com/acme/repo.git","contentHash":"legacy-content-hash","installedAt":"2020-01-01T00:00:00.000Z","updatedAt":"2020-01-01T00:00:00.000Z"`, field: "contentHash", fieldWant: "legacy-content-hash"},
		{version: 3, entry: `"sourceUrl":"https://example.com/acme/repo.git","skillFolderHash":"github-tree-sha","installedAt":"2020-01-01T00:00:00.000Z","updatedAt":"2020-01-01T00:00:00.000Z"`, field: "skillFolderHash", fieldWant: "github-tree-sha"},
	}
	for _, fixture := range fixtures {
		t.Run(strconv.Itoa(fixture.version), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", "")
			lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
			if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
				t.Fatal(err)
			}

			fixtureJSON := `{"version":` + strconv.Itoa(fixture.version) + `,"skills":{"my-skill":{"source":"acme/repo","sourceType":"github",` + fixture.entry + `,"entryExtension":{"preserved":true}}},"futureTopLevel":{"preserved":true}}`
			if err := os.WriteFile(lockPath, []byte(fixtureJSON), 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := lockfile.ReadGlobal()
			if err != nil {
				t.Fatalf("ReadGlobal() error = %v", err)
			}
			if got.Version != fixture.version {
				t.Errorf("version = %d, want %d", got.Version, fixture.version)
			}
			if fixture.field != "" {
				if value, ok := got.Skills["my-skill"].String(fixture.field); !ok || value != fixture.fieldWant {
					t.Errorf("entry[%q] = %q, %t; want %q", fixture.field, value, ok, fixture.fieldWant)
				}
			}

			after, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != fixtureJSON {
				t.Errorf("read rewrote lock file: got %s, want %s", after, fixtureJSON)
			}
		})
	}
}

func TestWriteGlobalNormalizesVersionAndPreservesUnknownFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"version":1,"skills":{"my-skill":{"source":"acme/repo","sourceType":"github","contentHash":"old-content-hash","entryExtension":{"preserved":true}}},"futureTopLevel":{"preserved":true}}`
	if err := os.WriteFile(lockPath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := lockfile.ReadGlobal()
	if err != nil {
		t.Fatalf("ReadGlobal() error = %v", err)
	}
	if err := got.SetSkill("my-skill", map[string]any{"skillFolderHash": "tree-sha-v3"}); err != nil {
		t.Fatalf("SetSkill() error = %v", err)
	}
	if err := lockfile.WriteGlobal(got); err != nil {
		t.Fatalf("WriteGlobal() error = %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var nodeLock map[string]json.RawMessage
	if err := json.Unmarshal(data, &nodeLock); err != nil {
		t.Fatalf("written lock is not Node-readable JSON: %v", err)
	}
	var version int
	if err := json.Unmarshal(nodeLock["version"], &version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Errorf("written version = %d, want 3", version)
	}
	if !containsJSONValue(t, nodeLock["futureTopLevel"], `{"preserved":true}`) {
		t.Error("unknown top-level field was not preserved")
	}
	var skills map[string]json.RawMessage
	if err := json.Unmarshal(nodeLock["skills"], &skills); err != nil {
		t.Fatal(err)
	}
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(skills["my-skill"], &entry); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"contentHash":     "old-content-hash",
		"skillFolderHash": "tree-sha-v3",
	} {
		var value string
		if err := json.Unmarshal(entry[key], &value); err != nil || value != want {
			t.Errorf("entry[%q] = %q, %v; want %q", key, value, err, want)
		}
	}
	if !containsJSONValue(t, entry["entryExtension"], `{"preserved":true}`) {
		t.Error("unknown entry field was not preserved")
	}
}

func TestReadAndWriteProjectPreservesPortableLocalSourcesAndUnknownFields(t *testing.T) {
	project := t.TempDir()
	lockPath := filepath.Join(project, "skills-lock.json")
	fixture := `{"version":1,"skills":{"z-skill":{"source":"./skills/z","sourceType":"local","computedHash":"z-hash","entryExtension":{"preserved":true}},"a-skill":{"source":"acme/repo","sourceType":"github","computedHash":"a-hash"}},"futureTopLevel":{"preserved":true}}`
	if err := os.WriteFile(lockPath, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := lockfile.ReadProject(project)
	if err != nil {
		t.Fatalf("ReadProject() error = %v", err)
	}
	localSource, ok := got.Skills["z-skill"].String("source")
	if !ok || localSource != filepath.Join(project, "skills", "z") {
		t.Fatalf("resolved local source = %q, %t; want absolute project-relative path", localSource, ok)
	}
	readOnlyData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(readOnlyData) != fixture {
		t.Error("reading the project lock rewrote the file")
	}
	if err := got.SetSkill("a-skill", map[string]any{"computedHash": "updated-hash"}); err != nil {
		t.Fatalf("SetSkill() error = %v", err)
	}
	if err := lockfile.WriteProject(project, got); err != nil {
		t.Fatalf("WriteProject() error = %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("project lock should end with a newline")
	}
	var nodeLock map[string]json.RawMessage
	if err := json.Unmarshal(data, &nodeLock); err != nil {
		t.Fatalf("written project lock is not Node-readable JSON: %v", err)
	}
	if !containsJSONValue(t, nodeLock["futureTopLevel"], `{"preserved":true}`) {
		t.Error("unknown top-level field was not preserved")
	}
	var skills map[string]json.RawMessage
	if err := json.Unmarshal(nodeLock["skills"], &skills); err != nil {
		t.Fatal(err)
	}
	if keys := []string{"a-skill", "z-skill"}; !jsonKeysEqual(t, nodeLock["skills"], keys) {
		t.Errorf("project skills are not written in sorted order: %v", keys)
	}
	var localEntry map[string]json.RawMessage
	if err := json.Unmarshal(skills["z-skill"], &localEntry); err != nil {
		t.Fatal(err)
	}
	var storedSource string
	if err := json.Unmarshal(localEntry["source"], &storedSource); err != nil || storedSource != "./skills/z" {
		t.Errorf("stored local source = %q, %v; want ./skills/z", storedSource, err)
	}
	if !containsJSONValue(t, localEntry["entryExtension"], `{"preserved":true}`) {
		t.Error("unknown entry field was not preserved")
	}

	reloaded, err := lockfile.ReadProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if source, _ := reloaded.Skills["z-skill"].String("source"); source != filepath.Join(project, "skills", "z") {
		t.Errorf("reloaded local source = %q, want absolute path", source)
	}
	if hash, _ := reloaded.Skills["a-skill"].String("computedHash"); hash != "updated-hash" {
		t.Errorf("reloaded computedHash = %q, want updated-hash", hash)
	}
}

func TestMissingAndMalformedLocksReturnEmptyWithoutCreatingFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	global, err := lockfile.ReadGlobal()
	if err != nil {
		t.Fatalf("ReadGlobal() for missing file: %v", err)
	}
	if global.Version != 3 || len(global.Skills) != 0 {
		t.Errorf("missing global lock = version %d, %d skills; want empty v3", global.Version, len(global.Skills))
	}
	if _, err := os.Stat(lockfile.GlobalPath()); !os.IsNotExist(err) {
		t.Errorf("read created global lock: stat error = %v", err)
	}

	globalPath := lockfile.GlobalPath()
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	malformed := []byte(`{"version":3,"skills":{broken}`)
	if err := os.WriteFile(globalPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	global, err = lockfile.ReadGlobal()
	if err != nil || global.Version != 3 || len(global.Skills) != 0 {
		t.Errorf("malformed global lock = %#v, %v; want empty v3", global, err)
	}
	after, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(malformed) {
		t.Error("reading malformed global lock rewrote the file")
	}

	project := t.TempDir()
	projectLockPath := filepath.Join(project, "skills-lock.json")
	projectLock, err := lockfile.ReadProject(project)
	if err != nil {
		t.Fatalf("ReadProject() for missing file: %v", err)
	}
	if projectLock.Version != 1 || len(projectLock.Skills) != 0 {
		t.Errorf("missing project lock = version %d, %d skills; want empty v1", projectLock.Version, len(projectLock.Skills))
	}
	if _, err := os.Stat(projectLockPath); !os.IsNotExist(err) {
		t.Errorf("read created project lock: stat error = %v", err)
	}
	if err := os.WriteFile(projectLockPath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	projectLock, err = lockfile.ReadProject(project)
	if err != nil || projectLock.Version != 1 || len(projectLock.Skills) != 0 {
		t.Errorf("malformed project lock = %#v, %v; want empty v1", projectLock, err)
	}
	projectAfter, err := os.ReadFile(projectLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(projectAfter) != string(malformed) {
		t.Error("reading malformed project lock rewrote the file")
	}
}

func TestGlobalPathUsesXDGStateHome(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if got, want := lockfile.GlobalPath(), filepath.Join(stateHome, "skills", ".skill-lock.json"); got != want {
		t.Errorf("GlobalPath() = %q, want %q", got, want)
	}
}

func TestComputeProjectHashMatchesContentHashContractAndExclusions(t *testing.T) {
	project := t.TempDir()
	for name, content := range map[string]string{
		"SKILL.md":                      "alpha",
		"nested/helper.md":              "beta",
		".git/HEAD":                     "ignored git metadata",
		"node_modules/package/index.js": "ignored dependency",
	} {
		path := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := lockfile.ComputeProjectHash(project)
	if err != nil {
		t.Fatalf("ComputeProjectHash() error = %v", err)
	}
	const want = "23e3ac57a5516e9b718fb72143ce66fdd8b75863196ee634cb58c5c25de26b9e"
	if got != want {
		t.Errorf("ComputeProjectHash() = %q, want %q", got, want)
	}

	for _, name := range []string{".git/HEAD", "node_modules/package/index.js"} {
		path := filepath.Join(project, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte("changed excluded file"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	afterExcludedChanges, err := lockfile.ComputeProjectHash(project)
	if err != nil {
		t.Fatalf("ComputeProjectHash() after excluded files changed: %v", err)
	}
	if afterExcludedChanges != want {
		t.Errorf("hash changed after excluded files changed: got %q, want %q", afterExcludedChanges, want)
	}

	if err := os.WriteFile(filepath.Join(project, "nested", "helper.md"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := lockfile.ComputeProjectHash(project)
	if err != nil {
		t.Fatal(err)
	}
	if changed == want {
		t.Error("hash did not change when an included file changed")
	}
}

func containsJSONValue(t *testing.T, raw json.RawMessage, want string) bool {
	t.Helper()
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("decode JSON value: %v", err)
		return false
	}
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.Marshal(got)
	expectedJSON, _ := json.Marshal(expected)
	return string(gotJSON) == string(expectedJSON)
}

func jsonKeysEqual(t *testing.T, raw json.RawMessage, want []string) bool {
	t.Helper()
	var skills map[string]json.RawMessage
	if err := json.Unmarshal(raw, &skills); err != nil {
		t.Errorf("decode skills: %v", err)
		return false
	}
	previous := -1
	for _, key := range want {
		if _, ok := skills[key]; !ok {
			t.Errorf("skills do not contain %q", key)
			return false
		}
		position := bytes.Index(raw, []byte(`"`+key+`":`))
		if position <= previous {
			return false
		}
		previous = position
	}
	return len(skills) == len(want)
}
