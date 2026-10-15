package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vercel-labs/skills/go/skillscan"
)

const (
	maxRemoteBytes    = 50 << 20
	maxRemoteFiles    = 1000
	wellKnownSchemaV2 = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"
)

type addSourceSession struct {
	scan      *skillscan.Scan
	source    skillscan.Source
	tempDir   string
	direct    bool
	wellKnown bool
}

func (session *addSourceSession) Close() {
	if session == nil {
		return
	}
	if session.scan != nil {
		_ = session.scan.Close()
	}
	if session.tempDir != "" {
		_ = os.RemoveAll(session.tempDir)
	}
}

func openAddSource(ctx context.Context, input string, options skillscan.Options) (*addSourceSession, error) {
	if isHTTPSource(input) && shouldDownloadHTTPSource(input) {
		tempDir, err := os.MkdirTemp("", "skills-download-")
		if err != nil {
			return nil, fmt.Errorf("create download directory: %w", err)
		}
		found := false
		if !isDirectArtifactURL(input) {
			found, err = fetchWellKnownSource(input, tempDir)
			if err != nil {
				_ = os.RemoveAll(tempDir)
				return nil, err
			}
		}
		if !found {
			if err := downloadDirectSource(input, tempDir); err != nil {
				_ = os.RemoveAll(tempDir)
				return nil, err
			}
		}
		scan, err := skillscan.Open(ctx, tempDir, options)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, err
		}
		return &addSourceSession{
			scan: scan, tempDir: tempDir, direct: !found, wellKnown: found,
			source: skillscan.Source{Kind: skillscan.GitSource, URL: input},
		}, nil
	}
	scan, err := skillscan.Open(ctx, input, options)
	if err != nil {
		return nil, err
	}
	return &addSourceSession{scan: scan, source: scan.Source()}, nil
}

func isHTTPSource(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

func shouldDownloadHTTPSource(input string) bool {
	parsed, err := url.Parse(input)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	pathname := strings.ToLower(parsed.Path)
	if host == "raw.githubusercontent.com" || host == "codeload.github.com" || host == "objects.githubusercontent.com" {
		return true
	}
	if host == "github.com" {
		return strings.Contains(pathname, "/archive/") || strings.Contains(pathname, "/raw/") || strings.Contains(pathname, "/releases/download/") || strings.Contains(pathname, "/releases/latest/download/")
	}
	if strings.Contains(pathname, "/-/archive/") || strings.Contains(pathname, "/-/raw/") {
		return true
	}
	if strings.HasSuffix(pathname, ".git") || strings.Contains(pathname, "/tree/") || strings.Contains(pathname, "/-/tree/") {
		return false
	}
	return host != "github.com" && host != "gitlab.com"
}

func isDirectArtifactURL(input string) bool {
	parsed, err := url.Parse(input)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	pathname := strings.ToLower(parsed.Path)
	if host == "raw.githubusercontent.com" || host == "codeload.github.com" || host == "objects.githubusercontent.com" {
		return true
	}
	if host == "github.com" && (strings.Contains(pathname, "/archive/") || strings.Contains(pathname, "/raw/") || strings.Contains(pathname, "/releases/download/") || strings.Contains(pathname, "/releases/latest/download/")) {
		return true
	}
	if strings.Contains(pathname, "/-/archive/") || strings.Contains(pathname, "/-/raw/") {
		return true
	}
	for _, suffix := range []string{".skill.md", "/skill.md", ".md", ".zip", ".tar.gz", ".tgz"} {
		if strings.HasSuffix(pathname, suffix) {
			return true
		}
	}
	return false
}

func fetchWellKnownSource(input, destination string) (bool, error) {
	parsed, err := url.Parse(input)
	if err != nil {
		return false, fmt.Errorf("invalid HTTP source %q: %w", input, err)
	}
	basePath := strings.TrimSuffix(parsed.Path, "/")
	wellKnownPaths := []string{".well-known/agent-skills", ".well-known/skills"}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, wellKnownPath := range wellKnownPaths {
		candidates := []string{joinURLPath(parsed, basePath, wellKnownPath, "index.json")}
		if basePath != "" {
			candidates = append(candidates, joinURLPath(parsed, "", wellKnownPath, "index.json"))
		}
		for _, indexURL := range candidates {
			body, status, fetchErr := getRemoteBytes(client, indexURL)
			if fetchErr != nil || status != http.StatusOK {
				continue
			}
			var index struct {
				Schema string            `json:"$schema"`
				Skills []json.RawMessage `json:"skills"`
			}
			if json.Unmarshal(body, &index) != nil || len(index.Skills) == 0 {
				continue
			}
			indexParsed, _ := url.Parse(indexURL)
			indexBase := indexParsed.Scheme + "://" + indexParsed.Host + strings.TrimSuffix(indexParsed.Path, "/"+wellKnownPath+"/index.json")
			if index.Schema == "" {
				if err := fetchLegacyWellKnown(client, destination, indexBase, wellKnownPath, index.Skills); err != nil || !containsSkillMarkdown(destination) {
					continue
				}
				return true, nil
			}
			if index.Schema == wellKnownSchemaV2 {
				if err := fetchV2WellKnown(client, destination, indexURL, index.Skills); err != nil || !containsSkillMarkdown(destination) {
					continue
				}
				return true, nil
			}
		}
	}
	return false, nil
}

func containsSkillMarkdown(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(filename string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(entry.Name(), "SKILL.md") {
			found = true
		}
		return nil
	})
	return found
}

func fetchLegacyWellKnown(client *http.Client, destination, baseURL, wellKnownPath string, rawEntries []json.RawMessage) error {
	for _, raw := range rawEntries {
		var entry struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Files       []string `json:"files"`
		}
		if json.Unmarshal(raw, &entry) != nil || !validWellKnownSkillName(entry.Name) || entry.Description == "" {
			continue
		}
		skillDirectory := filepath.Join(destination, "skills", entry.Name)
		for _, filename := range entry.Files {
			cleaned, ok := safeRemotePath(filename)
			if !ok {
				continue
			}
			fileURL := strings.TrimSuffix(baseURL, "/") + "/" + wellKnownPath + "/" + entry.Name + "/" + path.Clean(filename)
			body, status, err := getRemoteBytes(client, fileURL)
			if err != nil || status != http.StatusOK {
				continue
			}
			if strings.EqualFold(cleaned, "skill.md") {
				cleaned = "SKILL.md"
			}
			if err := writeRemoteFile(destination, filepath.Join(skillDirectory, filepath.FromSlash(cleaned)), body); err != nil {
				return err
			}
		}
	}
	return nil
}

func fetchV2WellKnown(client *http.Client, destination, indexURL string, rawEntries []json.RawMessage) error {
	for _, raw := range rawEntries {
		var entry struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Type        string `json:"type"`
			URL         string `json:"url"`
			Digest      string `json:"digest"`
		}
		if json.Unmarshal(raw, &entry) != nil || !validWellKnownSkillName(entry.Name) || entry.Description == "" || (entry.Type != "skill-md" && entry.Type != "archive") {
			continue
		}
		artifactURL, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		base, _ := url.Parse(indexURL)
		body, status, err := getRemoteBytes(client, base.ResolveReference(artifactURL).String())
		if err != nil || status != http.StatusOK {
			continue
		}
		digest := sha256.Sum256(body)
		if entry.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
			continue
		}
		skillDirectory := filepath.Join(destination, "skills", entry.Name)
		if entry.Type == "skill-md" {
			if err := writeRemoteFile(destination, filepath.Join(skillDirectory, "SKILL.md"), body); err != nil {
				return err
			}
		} else if err := extractRemoteArchive(body, entry.URL, skillDirectory); err != nil {
			continue
		}
	}
	return nil
}

func validWellKnownSkillName(name string) bool {
	if name == "" || len(name) > 64 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
			return false
		}
	}
	return true
}

func joinURLPath(base *url.URL, parts ...string) string {
	copyURL := *base
	joined := strings.TrimSuffix(copyURL.Path, "/")
	for _, part := range parts {
		if part != "" {
			joined += "/" + strings.Trim(part, "/")
		}
	}
	copyURL.Path = joined
	copyURL.RawQuery = ""
	copyURL.Fragment = ""
	return copyURL.String()
}

func getRemoteBytes(client *http.Client, source string) ([]byte, int, error) {
	request, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.ContentLength > maxRemoteBytes {
		return nil, response.StatusCode, fmt.Errorf("remote source exceeds %d bytes", maxRemoteBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteBytes+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(body) > maxRemoteBytes {
		return nil, response.StatusCode, fmt.Errorf("remote source exceeds %d bytes", maxRemoteBytes)
	}
	return body, response.StatusCode, nil
}

func downloadDirectSource(source, destination string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	body, status, err := getRemoteBytes(client, source)
	if err != nil {
		return fmt.Errorf("download source %q: %w", source, err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("download source %q returned HTTP %d", source, status)
	}
	if looksLikeSkillMarkdown(body) {
		return writeRemoteFile(destination, filepath.Join(destination, "SKILL.md"), body)
	}
	if err := extractRemoteArchive(body, source, destination); err != nil {
		return fmt.Errorf("downloaded URL is not a valid SKILL.md file or supported archive: %w", err)
	}
	return nil
}

func looksLikeSkillMarkdown(body []byte) bool {
	text := strings.TrimPrefix(string(body), string([]byte{0xef, 0xbb, 0xbf}))
	return strings.HasPrefix(strings.TrimSpace(text), "---") && strings.Contains(text, "name:") && strings.Contains(text, "description:")
}

func safeRemotePath(filename string) (string, bool) {
	filename = strings.ReplaceAll(filename, "\\", "/")
	if filename == "" || strings.HasPrefix(filename, "/") || strings.ContainsRune(filename, 0) || hasWindowsDrivePrefix(filename) {
		return "", false
	}
	cleaned := path.Clean(filename)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", false
	}
	return cleaned, true
}

func hasWindowsDrivePrefix(filename string) bool {
	return len(filename) >= 2 && ((filename[0] >= 'a' && filename[0] <= 'z') || (filename[0] >= 'A' && filename[0] <= 'Z')) && filename[1] == ':'
}

func writeRemoteFile(root, filename string, body []byte) error {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absoluteFile, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absoluteRoot, absoluteFile)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write outside download directory: %s", filename)
	}
	if err := os.MkdirAll(filepath.Dir(absoluteFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(absoluteFile, body, 0o600)
}

func extractRemoteArchive(body []byte, source, destination string) error {
	if len(body) < 4 {
		return fmt.Errorf("archive is too short")
	}
	lower := strings.ToLower(source)
	if bytes.HasPrefix(body, []byte{'P', 'K', 3, 4}) || strings.HasSuffix(lower, ".zip") {
		return extractRemoteZip(body, destination)
	}
	if bytes.HasPrefix(body, []byte{0x1f, 0x8b}) || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return extractRemoteTarGzip(body, destination)
	}
	return fmt.Errorf("unsupported archive format")
}

func extractRemoteZip(body []byte, destination string) error {
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	if len(archive.File) > maxRemoteFiles {
		return fmt.Errorf("archive contains too many files")
	}
	total := int64(0)
	for _, file := range archive.File {
		cleaned, ok := safeRemotePath(file.Name)
		if !ok {
			continue
		}
		output := filepath.Join(destination, filepath.FromSlash(cleaned))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(output, 0o700); err != nil {
				return err
			}
			continue
		}
		if file.UncompressedSize64 > uint64(maxRemoteBytes) || total > maxRemoteBytes-int64(file.UncompressedSize64) {
			return fmt.Errorf("archive expands beyond %d bytes", maxRemoteBytes)
		}
		total += int64(file.UncompressedSize64)
		if total > maxRemoteBytes {
			return fmt.Errorf("archive expands beyond %d bytes", maxRemoteBytes)
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, maxRemoteBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := writeRemoteFile(destination, output, contents); err != nil {
			return err
		}
	}
	return nil
}

func extractRemoteTarGzip(body []byte, destination string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	count := 0
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		count++
		if count > maxRemoteFiles {
			return fmt.Errorf("archive contains too many files")
		}
		cleaned, ok := safeRemotePath(header.Name)
		if !ok {
			continue
		}
		output := filepath.Join(destination, filepath.FromSlash(cleaned))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(output, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			contents, err := io.ReadAll(io.LimitReader(reader, maxRemoteBytes+1))
			if err != nil {
				return err
			}
			if err := writeRemoteFile(destination, output, contents); err != nil {
				return err
			}
		}
	}
}
