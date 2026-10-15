package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
)

// Mode selects how an installed skill relates to its source and agent directory.
type Mode string

const (
	Symlink Mode = "symlink"
	Copy    Mode = "copy"
	Link    Mode = "link"
)

// Skill describes a materialized skill directory and its discovery metadata.
type Skill struct {
	Name       string
	Path       string
	PluginName string
}

// Options configure one install without changing process-wide CLI state.
type Options struct {
	Global                 bool
	CWD                    string
	Mode                   Mode
	EveSubagent            string
	CreateMissingAgentRoot bool
	Warnings               io.Writer
	CreateSymlink          func(target, link string) error
}

// Result reports installation paths and agent-specific behavior.
type Result struct {
	Success       bool
	Path          string
	CanonicalPath string
	Mode          Mode
	PluginName    string
	SymlinkFailed bool
	Skipped       bool
	SkipReason    string
	Error         string
}

// InstallSkill installs one directory-based skill for one agent. Filesystem
// failures are returned as errors and are also copied into Result.Error for
// callers that serialize the installation result.
func InstallSkill(skill Skill, agentID agent.ID, options Options) (Result, error) {
	mode := options.Mode
	if mode == "" {
		mode = Symlink
	}
	result := Result{Mode: mode, PluginName: skill.PluginName}
	if mode != Symlink && mode != Copy && mode != Link {
		return failed(result, fmt.Errorf("unsupported install mode %q", mode))
	}

	name := skill.Name
	if name == "" {
		name = filepath.Base(skill.Path)
	}
	target, err := agent.ResolveTarget(agentID, name, agent.TargetOptions{
		Global: options.Global, CWD: options.CWD, EveSubagent: options.EveSubagent,
		CreateMissingAgentRoot: options.CreateMissingAgentRoot,
	})
	if err != nil {
		return failed(result, err)
	}
	result.Path = target.AgentDir

	source, err := filepath.Abs(skill.Path)
	if err != nil {
		return failed(result, fmt.Errorf("resolve skill source %q: %w", skill.Path, err))
	}

	if mode == Copy {
		if pathsOverlap(source, target.AgentDir) {
			result.Success = true
			result.Skipped = true
			return result, nil
		}
		if err := cleanAndCreate(target.AgentDir, source, target.CanonicalDir, true); err != nil {
			return failed(result, err)
		}
		if err := copyDirectory(source, target.AgentDir, agentID, options.Warnings); err != nil {
			return failed(result, fmt.Errorf("copy skill to %s: %w", target.AgentDir, err))
		}
		result.Success = true
		result.Path = target.AgentDir
		result.CanonicalPath = ""
		return result, nil
	}

	if pathsOverlap(source, target.AgentDir) {
		result.Success = true
		result.Skipped = true
		return result, nil
	}
	if pathsOverlap(source, target.CanonicalDir) {
		result.Success = true
		result.Path = target.CanonicalDir
		result.CanonicalPath = target.CanonicalDir
		result.Skipped = true
		return result, nil
	}
	if err := cleanAndCreate(target.CanonicalDir, source, target.AgentDir, true); err != nil {
		return failed(result, err)
	}

	if mode == Link {
		created, linkErr := createSymlink(source, target.CanonicalDir, source, target.AgentDir, options.CreateSymlink)
		if linkErr != nil {
			return failed(result, linkErr)
		}
		if !created {
			if err := cleanAndCreate(target.CanonicalDir, source, target.AgentDir, true); err != nil {
				return failed(result, err)
			}
			if err := copyDirectory(source, target.CanonicalDir, agentID, options.Warnings); err != nil {
				return failed(result, fmt.Errorf("copy skill to %s: %w", target.CanonicalDir, err))
			}
		}
	} else {
		if err := copyDirectory(source, target.CanonicalDir, agentID, options.Warnings); err != nil {
			return failed(result, fmt.Errorf("copy skill to %s: %w", target.CanonicalDir, err))
		}
	}

	if options.Global && target.Universal {
		result.Success = true
		result.Path = target.CanonicalDir
		result.CanonicalPath = target.CanonicalDir
		return result, nil
	}
	if target.SkipAgentLink {
		result.Success = true
		result.Path = target.CanonicalDir
		result.CanonicalPath = target.CanonicalDir
		result.Skipped = true
		result.SkipReason = agent.MissingAgentProjectDirectory
		return result, nil
	}

	created, linkErr := createSymlink(target.CanonicalDir, target.AgentDir, source, target.CanonicalDir, options.CreateSymlink)
	if linkErr != nil {
		return failed(result, linkErr)
	}
	if !created {
		if err := cleanAndCreate(target.AgentDir, source, target.CanonicalDir, false); err != nil {
			return failed(result, err)
		}
		if err := copyDirectory(source, target.AgentDir, agentID, options.Warnings); err != nil {
			return failed(result, fmt.Errorf("copy skill to %s: %w", target.AgentDir, err))
		}
		result.Success = true
		result.Path = target.AgentDir
		result.CanonicalPath = target.CanonicalDir
		result.SymlinkFailed = true
		return result, nil
	}

	result.Success = true
	result.Path = target.AgentDir
	result.CanonicalPath = target.CanonicalDir
	return result, nil
}

func failed(result Result, err error) (Result, error) {
	result.Success = false
	result.Error = err.Error()
	return result, err
}

func cleanAndCreate(path, source, protected string, allowSameProtected bool) error {
	if pathsOverlap(path, source) {
		return fmt.Errorf("refusing to remove %s because it overlaps skill source %s", path, source)
	}
	if protected != "" && pathsOverlap(path, protected) && !(allowSameProtected && samePath(path, protected)) {
		return fmt.Errorf("refusing to remove %s because it overlaps protected install path %s", path, protected)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove existing install %s: %w", path, err)
	}
	if err := os.MkdirAll(path, 0o777); err != nil {
		return fmt.Errorf("create install directory %s: %w", path, err)
	}
	return nil
}

func createSymlink(target, linkPath, source, protected string, creator func(string, string) error) (bool, error) {
	if samePath(target, linkPath) {
		return true, nil
	}
	if pathsOverlap(linkPath, source) {
		return false, fmt.Errorf("refusing to replace %s because it overlaps skill source %s", linkPath, source)
	}
	if protected != "" && pathsOverlap(linkPath, protected) && !samePath(linkPath, protected) {
		return false, fmt.Errorf("refusing to replace %s because it overlaps protected install path %s", linkPath, protected)
	}
	if resolvedLink, err := filepath.EvalSymlinks(linkPath); err == nil {
		if resolvedTarget, targetErr := filepath.EvalSymlinks(target); targetErr == nil && samePath(resolvedLink, resolvedTarget) {
			return true, nil
		}
	}

	if _, err := os.Lstat(linkPath); err == nil {
		if err := os.RemoveAll(linkPath); err != nil {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, nil
	}
	linkDir := filepath.Dir(linkPath)
	if err := os.MkdirAll(linkDir, 0o777); err != nil {
		return false, nil
	}
	resolvedLinkDir := resolveParent(linkDir)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, nil
	}
	symlinkTarget := targetAbs
	if runtime.GOOS != "windows" {
		symlinkTarget, err = filepath.Rel(resolvedLinkDir, targetAbs)
		if err != nil {
			return false, nil
		}
	}
	if creator == nil {
		creator = os.Symlink
	}
	if err := creator(symlinkTarget, linkPath); err != nil {
		return false, nil
	}
	return true, nil
}

func resolveParent(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved
	}
	return absolute
}

func samePath(first, second string) bool {
	firstResolved := resolveExistingPath(first)
	secondResolved := resolveExistingPath(second)
	return filepath.Clean(firstResolved) == filepath.Clean(secondResolved)
}

func pathsOverlap(first, second string) bool {
	firstAbsolute, firstErr := filepath.Abs(first)
	secondAbsolute, secondErr := filepath.Abs(second)
	if firstErr == nil && secondErr == nil && (pathWithin(firstAbsolute, secondAbsolute) || pathWithin(secondAbsolute, firstAbsolute)) {
		return true
	}
	firstResolved := resolveExistingPath(first)
	secondResolved := resolveExistingPath(second)
	return pathWithin(firstResolved, secondResolved) || pathWithin(secondResolved, firstResolved)
}

func resolveExistingPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	absolute = filepath.Clean(absolute)
	current := absolute
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathWithin(base, target string) bool {
	base, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
