package skillscan

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func (scan *Scan) openGit(ctx context.Context) error {
	temporaryDirectory, err := os.MkdirTemp(scan.options.TempDir, "skills-go-scan-")
	if err != nil {
		return fmt.Errorf("create temporary Git scan directory: %w", err)
	}
	scan.root = temporaryDirectory
	scan.tempDir = temporaryDirectory
	scan.removeTemp = true
	if _, _, err := runGit(ctx, "", "init", "--quiet", temporaryDirectory); err != nil {
		return fmt.Errorf("initialize temporary Git repository: %w", err)
	}
	if _, _, err := runGit(ctx, temporaryDirectory, "remote", "add", "origin", scan.source.URL); err != nil {
		return fmt.Errorf("configure Git source %q: %w", scan.source.URL, err)
	}

	fetchArgs := []string{"-c", "protocol.version=2", "fetch", "--depth=1", "--filter=blob:none", "--no-tags", "origin"}
	if scan.source.Ref == "" {
		fetchArgs = append(fetchArgs, "HEAD")
	} else {
		fetchArgs = append(fetchArgs, scan.source.Ref)
	}
	_, fetchStderr, err := runGit(ctx, temporaryDirectory, fetchArgs...)
	filterRejected := err != nil && isFilterRejection(fetchStderr+" "+err.Error())
	if filterRejected {
		_, _, _ = runGit(ctx, temporaryDirectory, "config", "--unset-all", "remote.origin.partialclonefilter")
		fallbackArgs := []string{"-c", "protocol.version=2", "fetch", "--depth=1", "--no-tags", "origin"}
		if scan.source.Ref == "" {
			fallbackArgs = append(fallbackArgs, "HEAD")
		} else {
			fallbackArgs = append(fallbackArgs, scan.source.Ref)
		}
		_, fallbackStderr, fallbackErr := runGit(ctx, temporaryDirectory, fallbackArgs...)
		fetchStderr += "\n" + fallbackStderr
		err = fallbackErr
	}
	if err != nil {
		return fmt.Errorf("fetch Git source %q: %w: %s", scan.source.URL, err, strings.TrimSpace(fetchStderr))
	}
	commit, _, err := runGit(ctx, temporaryDirectory, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve fetched Git revision: %w", err)
	}
	scan.commit = strings.TrimSpace(commit)
	if _, _, err := runGit(ctx, temporaryDirectory, "update-ref", "refs/skills-go/scan", scan.commit); err != nil {
		return fmt.Errorf("record fetched Git revision: %w", err)
	}

	tree, _, err := runGit(ctx, temporaryDirectory, "ls-tree", "-rz", "--full-tree", scan.commit)
	if err != nil {
		return fmt.Errorf("enumerate Git source tree: %w", err)
	}
	if err := scan.indexGitTree(tree); err != nil {
		return err
	}

	missing, _, err := runGit(ctx, temporaryDirectory, "rev-list", "--objects", "--missing=print", "--no-object-names", scan.commit)
	if err != nil {
		return fmt.Errorf("inspect partial Git objects: %w", err)
	}
	missingCount := 0
	for _, line := range strings.Split(missing, "\n") {
		if strings.HasPrefix(line, "?") {
			missingCount++
		}
	}
	scan.filterStatus = FilterStatus{Requested: true, Available: missingCount > 0 && !filterRejected, Fallback: missingCount == 0 || filterRejected}
	if filterRejected {
		scan.filterStatus.Message = "Git server rejected blob filtering; continuing with the complete shallow source."
	} else if strings.Contains(strings.ToLower(fetchStderr), "filtering not recognized") {
		scan.filterStatus.Available = false
		scan.filterStatus.Fallback = true
		scan.filterStatus.Message = "Git server does not support blob filtering; continuing with the complete shallow source."
	} else if missingCount == 0 {
		scan.filterStatus.Message = "Git server did not filter repository blobs; continuing with the complete shallow source."
	}
	return nil
}

func isFilterRejection(message string) bool {
	message = strings.ToLower(message)
	if !strings.Contains(message, "filter") {
		return false
	}
	for _, reason := range []string{"reject", "denied", "not recognized", "not supported", "unsupported", "not allowed", "not allow"} {
		if strings.Contains(message, reason) {
			return true
		}
	}
	return false
}

func (scan *Scan) indexGitTree(output string) error {
	for _, record := range bytes.Split([]byte(output), []byte{0}) {
		if len(record) == 0 {
			continue
		}
		metadata, treePath, found := strings.Cut(string(record), "\t")
		if !found {
			return fmt.Errorf("parse Git tree entry %q", record)
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 {
			return fmt.Errorf("parse Git tree entry metadata %q", metadata)
		}
		if fields[1] != "blob" {
			continue
		}
		scan.paths = append(scan.paths, treePath)
		scan.indexedFiles[treePath] = true
		scan.fileObjects[treePath] = fields[2]
	}
	sort.Strings(scan.paths)
	scan.indexDirectories()
	return nil
}

func (scan *Scan) readRepositoryFile(repositoryPath string) ([]byte, error) {
	if scan.source.Kind == LocalSource {
		return scan.readLocalFile(repositoryPath)
	}
	if cached, ok := scan.fileContents[repositoryPath]; ok {
		return cached, nil
	}
	object, ok := scan.fileObjects[repositoryPath]
	if !ok {
		return nil, fmt.Errorf("Git tree has no file %q", repositoryPath)
	}
	content, _, err := runGit(scan.ctx, scan.root, "cat-file", "blob", object)
	if err != nil {
		return nil, fmt.Errorf("read Git blob %q: %w", repositoryPath, err)
	}
	data := []byte(content)
	scan.fileContents[repositoryPath] = data
	return data, nil
}

func (scan *Scan) materializeGit(selected []Candidate) ([]MaterializedSkill, error) {
	rootSkill := false
	patterns := make([]string, 0, len(selected))
	for _, candidate := range selected {
		if candidate.Path == "." {
			rootSkill = true
			continue
		}
		pattern, err := sparsePattern(candidate.Path)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, pattern)
	}
	if rootSkill {
		if sparse, _, err := runGit(scan.ctx, scan.root, "config", "--bool", "--get", "core.sparseCheckout"); err == nil && strings.TrimSpace(sparse) == "true" {
			if _, _, err := runGit(scan.ctx, scan.root, "sparse-checkout", "disable"); err != nil {
				return nil, fmt.Errorf("disable sparse checkout for repository-root skill: %w", err)
			}
		}
		if _, _, err := runGit(scan.ctx, scan.root, "checkout", "--detach", scan.commit); err != nil {
			return nil, fmt.Errorf("materialize repository-root skill: %w", err)
		}
	} else if len(patterns) > 0 {
		sort.Strings(patterns)
		if _, _, err := runGit(scan.ctx, scan.root, "sparse-checkout", "init", "--no-cone"); err != nil {
			return nil, fmt.Errorf("enable non-cone sparse checkout: %w", err)
		}
		if _, _, err := runGitWithInput(scan.ctx, scan.root, strings.Join(patterns, "\n")+"\n", "sparse-checkout", "set", "--no-cone", "--stdin"); err != nil {
			return nil, fmt.Errorf("select sparse skill paths: %w", err)
		}
		if _, _, err := runGit(scan.ctx, scan.root, "checkout", "--detach", scan.commit); err != nil {
			return nil, fmt.Errorf("materialize selected Git skills: %w", err)
		}
	}

	result := make([]MaterializedSkill, 0, len(selected))
	for _, candidate := range selected {
		directory := scan.root
		if candidate.Path != "." {
			directory = filepath.Join(scan.root, filepath.FromSlash(candidate.Path))
		}
		result = append(result, MaterializedSkill{Candidate: candidate, Directory: directory})
	}
	return result, nil
}

func sparsePattern(skillPath string) (string, error) {
	cleaned, err := cleanSubpath(skillPath)
	if err != nil {
		return "", err
	}
	if cleaned == "" || cleaned == "." || path.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid sparse skill path %q", skillPath)
	}
	var escaped strings.Builder
	escaped.WriteByte('/')
	for _, character := range cleaned {
		switch character {
		case '\\', '*', '?', '[', ']':
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(character)
	}
	escaped.WriteByte('/')
	return escaped.String(), nil
}

func runGit(ctx context.Context, directory string, args ...string) (string, string, error) {
	return runGitWithInput(ctx, directory, "", args...)
}

func runGitWithInput(ctx context.Context, directory, input string, args ...string) (string, string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	if directory != "" {
		command.Dir = directory
	}
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), stderr.String(), nil
}
