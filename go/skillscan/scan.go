package skillscan

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	FullDepth             bool
	IncludeInternal       bool
	IncludeDuplicateNames bool
	TempDir               string
	Progress              io.Writer
}

type Candidate struct {
	Name        string
	Description string
	Path        string
	Content     string
	Internal    bool
	PluginName  string
}

type MaterializedSkill struct {
	Candidate Candidate
	Directory string
}

type FilterStatus struct {
	Requested bool
	Available bool
	Fallback  bool
	Message   string
}

type Scan struct {
	ctx               context.Context
	source            Source
	root              string
	tempDir           string
	removeTemp        bool
	options           Options
	paths             []string
	indexedFiles      map[string]bool
	directoryChildren map[string][]string
	fileContents      map[string][]byte
	fileObjects       map[string]string
	commit            string
	candidates        []Candidate
	byPath            map[string]Candidate
	lockedSkillNames  map[string]bool
	filterStatus      FilterStatus
	missingObjects    map[string]bool
	closed            bool
}

// Open creates a discovery session. Local directories are scanned in place;
// Git sources are fetched into a temporary, no-checkout repository.
func Open(ctx context.Context, input string, options Options) (*Scan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	source, err := ParseSource(input)
	if err != nil {
		return nil, err
	}
	scan := &Scan{
		ctx:               ctx,
		source:            source,
		options:           options,
		indexedFiles:      make(map[string]bool),
		directoryChildren: make(map[string][]string),
		fileContents:      make(map[string][]byte),
		fileObjects:       make(map[string]string),
		byPath:            make(map[string]Candidate),
	}
	if source.Kind == LocalSource {
		info, statErr := os.Stat(source.LocalPath)
		if statErr != nil {
			return nil, fmt.Errorf("read local source %q: %w", source.LocalPath, statErr)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("local Git source %q is not a directory", source.LocalPath)
		}
		scan.root = source.LocalPath
		if err := scan.indexLocalTree(); err != nil {
			return nil, err
		}
	} else {
		if err := scan.openGit(ctx); err != nil {
			if scan.removeTemp {
				_ = os.RemoveAll(scan.tempDir)
			}
			return nil, err
		}
		if options.Progress != nil {
			if err := scan.prefetchDiscoveryBlobs(); err != nil {
				fmt.Fprintf(options.Progress, "Warning: could not batch-fetch skill metadata; continuing with on-demand Git fetches: %v\n", err)
			}
		}
	}
	if err := scan.discover(); err != nil {
		_ = scan.Close()
		return nil, err
	}
	return scan, nil
}

func (scan *Scan) Source() Source { return scan.source }

func (scan *Scan) Candidates() []Candidate {
	return append([]Candidate(nil), scan.candidates...)
}

func (scan *Scan) FilterStatus() FilterStatus { return scan.filterStatus }

// GitTreeHash returns the Git object ID for a discovered directory, when the
// scan came from a Git repository. The root skill uses the repository root tree.
func (scan *Scan) GitTreeHash(directory string) (string, bool) {
	if scan.source.Kind != GitSource || scan.commit == "" {
		return "", false
	}
	object := scan.commit + "^{tree}"
	if directory != "" && directory != "." {
		cleaned, err := cleanSubpath(directory)
		if err != nil || cleaned == "" {
			return "", false
		}
		object = scan.commit + ":" + cleaned
	}
	resolved, _, err := runGit(scan.ctx, scan.root, "rev-parse", object)
	if err != nil {
		return "", false
	}
	resolved = strings.TrimSpace(resolved)
	kind, _, err := runGit(scan.ctx, scan.root, "cat-file", "-t", resolved)
	if err != nil || strings.TrimSpace(kind) != "tree" {
		return "", false
	}
	return resolved, true
}

// Materialize resolves selected repository-relative paths. A local source is
// returned in place; a Git source sparse-checks out only selected skill trees.
func (scan *Scan) Materialize(selectedPaths []string) ([]MaterializedSkill, error) {
	if scan.closed {
		return nil, fmt.Errorf("scan is already closed")
	}
	selected := make([]Candidate, 0, len(selectedPaths))
	seen := make(map[string]bool, len(selectedPaths))
	for _, requestedPath := range selectedPaths {
		candidate, ok := scan.byPath[requestedPath]
		if !ok {
			return nil, fmt.Errorf("unknown skill path %q", requestedPath)
		}
		if !seen[requestedPath] {
			selected = append(selected, candidate)
			seen[requestedPath] = true
		}
	}
	if len(selected) == 0 {
		return []MaterializedSkill{}, nil
	}
	if scan.source.Kind == LocalSource {
		return scan.localMaterialized(selected), nil
	}
	return scan.materializeGit(selected)
}

func (scan *Scan) localMaterialized(selected []Candidate) []MaterializedSkill {
	result := make([]MaterializedSkill, 0, len(selected))
	for _, candidate := range selected {
		directory := scan.root
		if candidate.Path != "." {
			directory = filepath.Join(scan.root, filepath.FromSlash(candidate.Path))
		}
		result = append(result, MaterializedSkill{Candidate: candidate, Directory: directory})
	}
	return result
}

// Close removes the temporary Git repository and all fetched objects.
func (scan *Scan) Close() error {
	if scan.closed {
		return nil
	}
	scan.closed = true
	if scan.removeTemp {
		return os.RemoveAll(scan.tempDir)
	}
	return nil
}

func (scan *Scan) indexLocalTree() error {
	var paths []string
	err := filepath.WalkDir(scan.root, func(fullPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		relative, err := filepath.Rel(scan.root, fullPath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		name := filepath.Base(fullPath)
		if entry.IsDir() && skippedDirectory(name) {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan local source %q: %w", scan.root, err)
	}
	sort.Strings(paths)
	scan.paths = paths
	for _, file := range paths {
		scan.indexedFiles[file] = true
	}
	scan.indexDirectories()
	return nil
}

func (scan *Scan) indexDirectories() {
	children := make(map[string]map[string]bool)
	for _, file := range scan.paths {
		parts := strings.Split(file, "/")
		for index := 0; index < len(parts)-1; index++ {
			parent := "."
			if index > 0 {
				parent = strings.Join(parts[:index], "/")
			}
			child := strings.Join(parts[:index+1], "/")
			if children[parent] == nil {
				children[parent] = make(map[string]bool)
			}
			children[parent][child] = true
		}
	}
	for parent, childSet := range children {
		for child := range childSet {
			scan.directoryChildren[parent] = append(scan.directoryChildren[parent], child)
		}
		sort.Strings(scan.directoryChildren[parent])
	}
}

func (scan *Scan) readLocalFile(relative string) ([]byte, error) {
	fullPath := filepath.Join(scan.root, filepath.FromSlash(relative))
	return os.ReadFile(fullPath)
}

func (scan *Scan) searchRoot() string {
	return scan.source.Subpath
}

func (scan *Scan) repoPath(searchPath string) string {
	if searchPath == "." || searchPath == "" {
		return scan.searchRoot()
	}
	if scan.searchRoot() == "" {
		return path.Clean(searchPath)
	}
	return path.Join(scan.searchRoot(), searchPath)
}
