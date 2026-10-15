// Package lockfile reads and writes the lock files shared by the Go and Node CLIs.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	GlobalVersion  = 3
	ProjectVersion = 1

	globalLockName  = ".skill-lock.json"
	projectLockName = "skills-lock.json"
)

// Entry contains the fields for a single skill and retains fields unknown to
// this version of the CLI when a lock is read and written again.
type Entry map[string]json.RawMessage

// String returns a string-valued entry field.
func (entry Entry) String(key string) (string, bool) {
	var value string
	if err := json.Unmarshal(entry[key], &value); err != nil {
		return "", false
	}
	return value, true
}

// Set encodes value as JSON in the named entry field.
func (entry Entry) Set(key string, value any) error {
	if entry == nil {
		return errors.New("cannot set a field on a nil lock entry")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode lock entry field %q: %w", key, err)
	}
	entry[key] = encoded
	return nil
}

// GlobalLock is the user-wide lock stored in the user's agents directory.
type GlobalLock struct {
	Version int
	Skills  map[string]Entry
	fields  map[string]json.RawMessage
}

// SetSkill adds or updates known fields while retaining fields not supplied.
func (lock *GlobalLock) SetSkill(name string, fields map[string]any) error {
	if lock.Skills == nil {
		lock.Skills = make(map[string]Entry)
	}
	return setSkill(lock.Skills, name, fields)
}

// ProjectLock is the project lock stored beside the project files.
type ProjectLock struct {
	Version int
	Skills  map[string]Entry
	fields  map[string]json.RawMessage
}

// SetSkill adds or updates known fields while retaining fields not supplied.
func (lock *ProjectLock) SetSkill(name string, fields map[string]any) error {
	if lock.Skills == nil {
		lock.Skills = make(map[string]Entry)
	}
	return setSkill(lock.Skills, name, fields)
}

func setSkill(skills map[string]Entry, name string, fields map[string]any) error {
	entry := skills[name]
	if entry == nil {
		entry = make(Entry)
	}
	for key, value := range fields {
		if err := entry.Set(key, value); err != nil {
			return err
		}
	}
	skills[name] = entry
	return nil
}

// GlobalPath returns the current Node CLI global lock path. XDG_STATE_HOME
// takes precedence over the default ~/.agents location.
func GlobalPath() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "skills", globalLockName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".agents", globalLockName)
	}
	return filepath.Join(home, ".agents", globalLockName)
}

// ReadGlobal reads Global lock versions 1 through 3. Missing or malformed
// files produce an empty v3 lock and are never rewritten as a side effect.
func ReadGlobal() (*GlobalLock, error) {
	path := GlobalPath()
	doc, err := readDocument(path)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.version < 1 {
		return emptyGlobal(), nil
	}
	return &GlobalLock{Version: doc.version, Skills: doc.skills, fields: doc.fields}, nil
}

// WriteGlobal writes the current Global schema (v3), retaining unknown fields
// that were present in the lock read by this package.
func WriteGlobal(lock *GlobalLock) error {
	if lock == nil {
		return errors.New("cannot write a nil Global lock")
	}
	lock.Version = GlobalVersion
	if lock.Skills == nil {
		lock.Skills = make(map[string]Entry)
	}
	return writeDocument(GlobalPath(), GlobalVersion, lock.Skills, lock.fields, true, false)
}

// ReadProject reads the v1 Project lock and resolves relative local sources
// from the lock file's project directory.
func ReadProject(projectDir string) (*ProjectLock, error) {
	path := filepath.Join(projectDir, projectLockName)
	doc, err := readDocument(path)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.version < ProjectVersion {
		return emptyProject(), nil
	}
	absoluteProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project directory %q: %w", projectDir, err)
	}
	for _, entry := range doc.skills {
		if sourceType, _ := entry.String("sourceType"); sourceType != "local" {
			continue
		}
		source, ok := entry.String("source")
		if !ok || filepath.IsAbs(source) {
			continue
		}
		if err := entry.Set("source", filepath.Clean(filepath.Join(absoluteProject, filepath.FromSlash(source)))); err != nil {
			return nil, err
		}
	}
	return &ProjectLock{Version: doc.version, Skills: doc.skills, fields: doc.fields}, nil
}

// WriteProject writes Project lock version 1, sorting skill names and storing
// local source paths relative to the project directory where possible.
func WriteProject(projectDir string, lock *ProjectLock) error {
	if lock == nil {
		return errors.New("cannot write a nil Project lock")
	}
	absoluteProject, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolve project directory %q: %w", projectDir, err)
	}
	skills := make(map[string]Entry, len(lock.Skills))
	for name, entry := range lock.Skills {
		copyEntry := make(Entry, len(entry))
		for key, value := range entry {
			copyEntry[key] = append(json.RawMessage(nil), value...)
		}
		if sourceType, _ := copyEntry.String("sourceType"); sourceType == "local" {
			if source, ok := copyEntry.String("source"); ok {
				portable, err := portableSource(source, absoluteProject)
				if err != nil {
					return err
				}
				if err := copyEntry.Set("source", portable); err != nil {
					return err
				}
			}
		}
		skills[name] = copyEntry
	}
	lock.Version = ProjectVersion
	if lock.Skills == nil {
		lock.Skills = make(map[string]Entry)
	}
	return writeDocument(filepath.Join(projectDir, projectLockName), ProjectVersion, skills, lock.fields, false, true)
}

func portableSource(source, projectDir string) (string, error) {
	absoluteSource := source
	if !filepath.IsAbs(absoluteSource) {
		absoluteSource = filepath.Join(projectDir, filepath.FromSlash(source))
	}
	absoluteSource, err := filepath.Abs(absoluteSource)
	if err != nil {
		return "", fmt.Errorf("resolve local skill source %q: %w", source, err)
	}
	relativeSource, err := filepath.Rel(projectDir, absoluteSource)
	if err != nil {
		return filepath.ToSlash(absoluteSource), nil
	}
	portable := filepath.ToSlash(relativeSource)
	if portable == "." || portable == ".." || strings.HasPrefix(portable, "../") {
		return portable, nil
	}
	return "./" + portable, nil
}

type document struct {
	version int
	skills  map[string]Entry
	fields  map[string]json.RawMessage
}

func readDocument(path string) (*document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read lock file %q: %w", path, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, nil
	}
	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil || version < 1 {
		return nil, nil
	}
	var skills map[string]Entry
	if err := json.Unmarshal(raw["skills"], &skills); err != nil || skills == nil {
		return nil, nil
	}

	fields := make(map[string]json.RawMessage, len(raw)-2)
	for key, value := range raw {
		if key == "version" || key == "skills" {
			continue
		}
		fields[key] = append(json.RawMessage(nil), value...)
	}
	return &document{version: version, skills: skills, fields: fields}, nil
}

func writeDocument(path string, version int, skills map[string]Entry, fields map[string]json.RawMessage, createParent bool, trailingNewline bool) error {
	if createParent {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create lock directory %q: %w", filepath.Dir(path), err)
		}
	}
	raw := make(map[string]json.RawMessage, len(fields)+2)
	for key, value := range fields {
		if key == "version" || key == "skills" {
			continue
		}
		raw[key] = append(json.RawMessage(nil), value...)
	}
	encodedVersion, err := json.Marshal(version)
	if err != nil {
		return err
	}
	encodedSkills, err := json.Marshal(skills)
	if err != nil {
		return fmt.Errorf("encode lock skills: %w", err)
	}
	raw["version"] = encodedVersion
	raw["skills"] = encodedSkills
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lock file %q: %w", path, err)
	}
	if trailingNewline {
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write lock file %q: %w", path, err)
	}
	return nil
}

func emptyGlobal() *GlobalLock {
	return &GlobalLock{Version: GlobalVersion, Skills: make(map[string]Entry)}
}

func emptyProject() *ProjectLock {
	return &ProjectLock{Version: ProjectVersion, Skills: make(map[string]Entry)}
}

// ComputeProjectHash computes the Project lock SHA-256 from sorted relative
// file paths followed by file contents, excluding .git and node_modules trees.
func ComputeProjectHash(skillDir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(skillDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == skillDir {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			relativePath, err := filepath.Rel(skillDir, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relativePath))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk skill directory %q: %w", skillDir, err)
	}
	sort.Strings(paths)

	hash := sha256.New()
	for _, relativePath := range paths {
		if _, err := hash.Write([]byte(relativePath)); err != nil {
			return "", err
		}
		content, err := os.ReadFile(filepath.Join(skillDir, filepath.FromSlash(relativePath)))
		if err != nil {
			return "", fmt.Errorf("read skill file %q: %w", filepath.Join(skillDir, relativePath), err)
		}
		if _, err := hash.Write(content); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
