package skillscan

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SourceKind string

const (
	LocalSource SourceKind = "local"
	GitSource   SourceKind = "git"
)

type Source struct {
	Kind        SourceKind
	URL         string
	LocalPath   string
	Ref         string
	Subpath     string
	SkillFilter string
}

var (
	shorthandPattern = regexp.MustCompile(`^[^/@:]+/[^/@:]+(?:/.+)?$`)
	sourceAliases    = map[string]string{
		"coinbase/agentWallet":      "coinbase/agentic-wallet-skills",
		"vercel-labs/vercel-skills": "vercel-labs/agent-skills",
	}
)

// ParseSource distinguishes local directories from Git sources and normalizes
// the common GitHub, GitLab, HTTPS, and SSH source forms.
func ParseSource(input string) (Source, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Source{}, fmt.Errorf("source is empty")
	}
	if local, ok := localSourcePath(input); ok {
		absolute, err := filepath.Abs(local)
		if err != nil {
			return Source{}, fmt.Errorf("resolve local source %q: %w", input, err)
		}
		return Source{Kind: LocalSource, URL: absolute, LocalPath: absolute}, nil
	}

	ref, skillFilter := "", ""
	if index := strings.LastIndex(input, "#"); index >= 0 {
		fragment := input[index+1:]
		input = input[:index]
		if at := strings.IndexByte(fragment, '@'); at >= 0 {
			ref, skillFilter = fragment[:at], fragment[at+1:]
		} else {
			ref = fragment
		}
	}

	if strings.HasPrefix(input, "github:") {
		shorthand := strings.TrimPrefix(input, "github:")
		if at := strings.IndexByte(shorthand, '@'); at >= 0 {
			if skillFilter == "" {
				skillFilter = shorthand[at+1:]
			}
			shorthand = shorthand[:at]
		}
		parts := strings.SplitN(shorthand, "/", 3)
		if len(parts) < 2 || parts[0] == "" || strings.TrimSuffix(parts[1], ".git") == "" {
			return Source{}, fmt.Errorf("invalid GitHub source %q", input)
		}
		repository := "https://github.com/" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git") + ".git"
		var subpath string
		if len(parts) == 3 {
			var err error
			subpath, err = cleanSubpath(parts[2])
			if err != nil {
				return Source{}, err
			}
		}
		return Source{Kind: GitSource, URL: repository, Ref: ref, Subpath: subpath, SkillFilter: skillFilter}, nil
	}
	if strings.HasPrefix(input, "gitlab:") {
		input = "https://gitlab.com/" + strings.TrimPrefix(input, "gitlab:")
	}

	if strings.HasPrefix(input, "git@") {
		return Source{Kind: GitSource, URL: input, Ref: ref, SkillFilter: skillFilter}, nil
	}
	if strings.HasPrefix(input, "ssh://") || strings.HasPrefix(input, "git://") || strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "file://") {
		return parseGitURL(input, ref, skillFilter)
	}

	if at := strings.IndexByte(input, '@'); at > 0 {
		if shorthandPattern.MatchString(input[:at]) {
			skillFilter = input[at+1:]
			input = input[:at]
		}
	}
	if alias, ok := sourceAliases[input]; ok {
		input = alias
	}
	if shorthandPattern.MatchString(input) {
		parts := strings.Split(input, "/")
		owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
		if repo == "" {
			return Source{}, fmt.Errorf("invalid Git source %q", input)
		}
		var subpath string
		if len(parts) > 2 {
			var err error
			subpath, err = cleanSubpath(strings.Join(parts[2:], "/"))
			if err != nil {
				return Source{}, err
			}
		}
		return Source{Kind: GitSource, URL: "https://github.com/" + owner + "/" + repo + ".git", Ref: ref, Subpath: subpath, SkillFilter: skillFilter}, nil
	}
	return Source{}, fmt.Errorf("unrecognized Git source %q", input)
}

func localSourcePath(input string) (string, bool) {
	if strings.HasPrefix(input, "~") {
		if input == "~" || strings.HasPrefix(input, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(input, "~"), string(filepath.Separator))), true
			}
		}
	}
	windowsDrivePath := len(input) >= 3 && ((input[0] >= 'a' && input[0] <= 'z') || (input[0] >= 'A' && input[0] <= 'Z')) && input[1] == ':' && (input[2] == '/' || input[2] == '\\')
	if filepath.IsAbs(input) || windowsDrivePath || strings.HasPrefix(input, ".") || input == ".." {
		return input, true
	}
	return "", false
}

func parseGitURL(input, fragmentRef, skillFilter string) (Source, error) {
	parsed, err := url.Parse(input)
	if err != nil || (parsed.Host == "" && parsed.Scheme != "file") || (parsed.Scheme == "file" && !filepath.IsAbs(parsed.Path)) {
		return Source{}, fmt.Errorf("invalid Git URL %q", input)
	}
	segments := splitURLPath(parsed.EscapedPath())
	ref := fragmentRef
	var subpath string

	if strings.EqualFold(parsed.Hostname(), "github.com") && len(segments) >= 4 && segments[2] == "tree" {
		ref = segments[3]
		if len(segments) > 4 {
			subpath = strings.Join(segments[4:], "/")
		}
		parsed.Path = "/" + segments[0] + "/" + strings.TrimSuffix(segments[1], ".git") + ".git"
		parsed.RawPath = ""
	} else if len(segments) >= 5 {
		marker := -1
		for i := 1; i < len(segments)-1; i++ {
			if segments[i] == "-" && segments[i+1] == "tree" {
				marker = i
				break
			}
		}
		if marker >= 1 {
			ref = segments[marker+2]
			if marker+3 < len(segments) {
				subpath = strings.Join(segments[marker+3:], "/")
			}
			repoPath := strings.Join(segments[:marker], "/")
			parsed.Path = "/" + strings.TrimSuffix(repoPath, ".git") + ".git"
			parsed.RawPath = ""
		}
	}

	if len(segments) == 2 && strings.EqualFold(parsed.Hostname(), "github.com") {
		parsed.Path = "/" + segments[0] + "/" + strings.TrimSuffix(segments[1], ".git") + ".git"
		parsed.RawPath = ""
	}
	if len(segments) >= 2 && strings.EqualFold(parsed.Hostname(), "gitlab.com") && !strings.Contains(input, "/-/tree/") {
		parsed.Path = "/" + strings.TrimSuffix(strings.Join(segments, "/"), ".git") + ".git"
		parsed.RawPath = ""
	}

	cleanedSubpath, err := cleanSubpath(subpath)
	if err != nil {
		return Source{}, err
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return Source{Kind: GitSource, URL: parsed.String(), Ref: ref, Subpath: cleanedSubpath, SkillFilter: skillFilter}, nil
}

func splitURLPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		decoded, err := url.PathUnescape(part)
		if err != nil {
			decoded = part
		}
		result = append(result, decoded)
	}
	return result
}

func cleanSubpath(subpath string) (string, error) {
	normalized := strings.ReplaceAll(subpath, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", fmt.Errorf("unsafe Git subpath %q contains '..'", subpath)
		}
	}
	return strings.Trim(normalized, "/"), nil
}
