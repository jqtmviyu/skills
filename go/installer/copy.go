package installer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vercel-labs/skills/go/agent"
)

func copyDirectory(source, destination string, agentID agent.ID, warnings io.Writer) error {
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	return copyDirectoryTree(source, destination, filepath.Clean(root), agentID, warnings)
}

func copyDirectoryTree(source, destination, destinationRoot string, agentID agent.ID, warnings io.Writer) error {
	if err := os.MkdirAll(destination, 0o777); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "metadata.json" || (entry.IsDir() && excludedDirectory(entry.Name())) {
			continue
		}
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		if !pathWithin(destinationRoot, destinationPath) {
			return fmt.Errorf("refusing to copy outside installation directory: %s", destinationPath)
		}
		if entry.IsDir() {
			if err := copyDirectoryTree(sourcePath, destinationPath, destinationRoot, agentID, warnings); err != nil {
				return err
			}
			continue
		}

		info, statErr := os.Stat(sourcePath)
		if statErr != nil {
			if entry.Type()&os.ModeSymlink != 0 && errors.Is(statErr, fs.ErrNotExist) {
				warnBrokenSymlink(warnings, sourcePath)
				continue
			}
			return statErr
		}
		if info.IsDir() {
			// Node's fs.cp dereferences symlinked directories recursively. Keep
			// that behavior, including the fact that custom exclusions only apply
			// to ordinary directory entries visited above.
			if err := copyDereferencedTree(sourcePath, destinationPath, destinationRoot, make(map[string]bool), warnings); err != nil {
				if entry.Type()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
					warnBrokenSymlink(warnings, sourcePath)
					continue
				}
				return err
			}
			continue
		}
		if agentID == "eve" && strings.EqualFold(entry.Name(), "SKILL.md") {
			contents, err := os.ReadFile(sourcePath)
			if err != nil {
				if entry.Type()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
					warnBrokenSymlink(warnings, sourcePath)
					continue
				}
				return err
			}
			filtered, err := stripIgnoredEveFrontmatter(string(contents))
			if err != nil {
				return err
			}
			if err := os.WriteFile(destinationPath, []byte(filtered), info.Mode().Perm()); err != nil {
				return err
			}
			if err := os.Chmod(destinationPath, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if err := copyRegularFile(sourcePath, destinationPath, info); err != nil {
			if entry.Type()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
				warnBrokenSymlink(warnings, sourcePath)
				continue
			}
			return err
		}
	}
	return nil
}

func copyDereferencedTree(source, destination, destinationRoot string, active map[string]bool, warnings io.Writer) error {
	info, err := os.Stat(source)
	if err != nil {
		linkInfo, linkErr := os.Lstat(source)
		if linkErr == nil && linkInfo.Mode()&os.ModeSymlink != 0 && errors.Is(err, fs.ErrNotExist) {
			warnBrokenSymlink(warnings, source)
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return copyRegularFile(source, destination, info)
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if active[resolved] {
		return fmt.Errorf("recursive directory symlink at %s", source)
	}
	active[resolved] = true
	defer delete(active, resolved)

	if !pathWithin(destinationRoot, destination) {
		return fmt.Errorf("refusing to copy outside installation directory: %s", destination)
	}
	if err := os.MkdirAll(destination, 0o777); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		if !pathWithin(destinationRoot, destinationPath) {
			return fmt.Errorf("refusing to copy outside installation directory: %s", destinationPath)
		}
		if err := copyDereferencedTree(sourcePath, destinationPath, destinationRoot, active, warnings); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(source, destination string, info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot copy unsupported file type %s (%s)", source, info.Mode().Type())
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o777); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666)
	if err != nil {
		_ = input.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	inputErr := input.Close()
	outputErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputErr != nil {
		return inputErr
	}
	if outputErr != nil {
		return outputErr
	}
	return os.Chmod(destination, info.Mode().Perm())
}

func excludedDirectory(name string) bool {
	switch name {
	case ".git", "__pycache__", "__pypackages__":
		return true
	default:
		return false
	}
}

func warnBrokenSymlink(warnings io.Writer, path string) {
	if warnings == nil {
		warnings = os.Stderr
	}
	_, _ = fmt.Fprintf(warnings, "Skipping broken symlink: %s\n", path)
}
