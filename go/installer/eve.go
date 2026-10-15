package installer

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var frontmatterPattern = regexp.MustCompile(`^(?:\xEF\xBB\xBF)?---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$`)

type eveFrontmatter struct {
	Name          *string         `yaml:"name,omitempty"`
	Description   *string         `yaml:"description,omitempty"`
	License       *string         `yaml:"license,omitempty"`
	Compatibility *any            `yaml:"compatibility,omitempty"`
	Version       *any            `yaml:"version,omitempty"`
	Metadata      *map[string]any `yaml:"metadata,omitempty"`
}

func stripIgnoredEveFrontmatter(raw string) (string, error) {
	content := raw
	data := map[string]any{}
	if match := frontmatterPattern.FindStringSubmatch(raw); match != nil {
		content = match[2]
		var parsed any
		if err := yaml.Unmarshal([]byte(match[1]), &parsed); err != nil {
			return "", fmt.Errorf("parse Eve skill frontmatter: %w", err)
		}
		switch fields := parsed.(type) {
		case map[string]any:
			data = fields
		case map[any]any:
			for key, value := range fields {
				if name, ok := key.(string); ok {
					data[name] = value
				}
			}
		}
	}

	filtered := eveFrontmatter{}
	if value, ok := data["name"].(string); ok {
		filtered.Name = &value
	}
	if value, ok := data["description"].(string); ok {
		filtered.Description = &value
	}
	if value, ok := data["license"].(string); ok {
		filtered.License = &value
	}
	if value, ok := data["compatibility"]; ok {
		filtered.Compatibility = &value
	}
	if value, ok := data["version"]; ok {
		switch value.(type) {
		case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			filtered.Version = &value
		}
	}
	if value, ok := data["metadata"].(map[string]any); ok {
		filtered.Metadata = &value
	}

	if filtered.Name == nil && filtered.Description == nil && filtered.License == nil && filtered.Compatibility == nil && filtered.Version == nil && filtered.Metadata == nil {
		return trimLeadingNewline(content), nil
	}
	frontmatter, err := yaml.Marshal(filtered)
	if err != nil {
		return "", fmt.Errorf("write Eve skill frontmatter: %w", err)
	}
	frontmatter = []byte(strings.TrimRight(string(frontmatter), "\r\n"))
	return "---\n" + string(frontmatter) + "\n---\n" + trimLeadingNewline(content), nil
}

func trimLeadingNewline(content string) string {
	if strings.HasPrefix(content, "\r\n") {
		return content[2:]
	}
	return strings.TrimPrefix(content, "\n")
}
