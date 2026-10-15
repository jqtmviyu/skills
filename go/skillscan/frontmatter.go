package skillscan

import (
	"strconv"
	"strings"
	"unicode"
)

type yamlValue struct {
	text     string
	isString bool
	isBool   bool
	boolean  bool
}

func parseSkillFrontmatter(raw string) (name, description string, internal, ok bool) {
	raw = strings.TrimPrefix(raw, string(rune(0xFEFF)))
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", false, false
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end < 0 {
		return "", "", false, false
	}

	var nameValue, descriptionValue yamlValue
	metadataIndent := -1
	for index := 1; index < end; index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := leadingSpaces(line)
		if indent == 0 {
			key, value, found := strings.Cut(trimmed, ":")
			if !found {
				return "", "", false, false
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			metadataIndent = -1
			switch key {
			case "name":
				nameValue = parseYAMLScalar(value)
			case "description":
				if value == "|" || value == ">" || strings.HasPrefix(value, "|- ") || strings.HasPrefix(value, ">- ") || value == "|-" || value == ">-" || value == "|+" || value == ">+" {
					block, next := parseBlockScalar(lines, index+1, end, value[0] == '>')
					descriptionValue = yamlValue{text: block, isString: true}
					index = next - 1
				} else {
					descriptionValue = parseYAMLScalar(value)
				}
			case "metadata":
				if value == "" {
					metadataIndent = indent
				}
			}
			continue
		}
		if metadataIndent >= 0 && indent > metadataIndent {
			key, value, found := strings.Cut(trimmed, ":")
			if found && strings.TrimSpace(key) == "internal" {
				parsed := parseYAMLScalar(strings.TrimSpace(value))
				internal = parsed.isBool && parsed.boolean
			}
		}
	}
	nameValue.text = sanitizeMetadata(nameValue.text)
	descriptionValue.text = sanitizeMetadata(descriptionValue.text)
	if !nameValue.isString || !descriptionValue.isString || nameValue.text == "" || descriptionValue.text == "" {
		return "", "", internal, false
	}
	return nameValue.text, descriptionValue.text, internal, true
}

func parseYAMLScalar(value string) yamlValue {
	value = stripYAMLComment(strings.TrimSpace(value))
	if value == "" || value == "~" || strings.EqualFold(value, "null") {
		return yamlValue{}
	}
	if value[0] == '"' {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return yamlValue{}
		}
		return yamlValue{text: decoded, isString: true}
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return yamlValue{}
		}
		return yamlValue{text: strings.ReplaceAll(value[1:len(value)-1], "''", "'"), isString: true}
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
		return yamlValue{}
	}
	if strings.EqualFold(value, "true") || strings.EqualFold(value, "false") {
		return yamlValue{isBool: true, boolean: strings.EqualFold(value, "true")}
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return yamlValue{}
	}
	return yamlValue{text: value, isString: true}
}

func stripYAMLComment(value string) string {
	quote := rune(0)
	escaped := false
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if (character == '"' || character == '\'') && (quote == 0 || quote == character) {
			if quote == 0 {
				quote = character
			} else {
				quote = 0
			}
			continue
		}
		if character == '#' && quote == 0 && index > 0 && unicode.IsSpace(rune(value[index-1])) {
			return strings.TrimSpace(value[:index])
		}
	}
	return value
}

func parseBlockScalar(lines []string, start, end int, folded bool) (string, int) {
	indent := -1
	var values []string
	index := start
	for ; index < end; index++ {
		line := lines[index]
		if strings.TrimSpace(line) == "" {
			values = append(values, "")
			continue
		}
		currentIndent := leadingSpaces(line)
		if indent < 0 {
			indent = currentIndent
		}
		if currentIndent < indent {
			break
		}
		values = append(values, line[indent:])
	}
	separator := "\n"
	if folded {
		separator = " "
	}
	return strings.TrimRight(strings.Join(values, separator), "\n"), index
}

func leadingSpaces(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func sanitizeMetadata(value string) string {
	characters := []rune(value)
	var result strings.Builder
	for index := 0; index < len(characters); index++ {
		character := characters[index]
		if character == '\x1b' {
			index = skipTerminalEscape(characters, index)
			continue
		}
		if unicode.IsControl(character) && character != '\t' && character != '\n' {
			continue
		}
		result.WriteRune(character)
	}
	cleaned := strings.ReplaceAll(result.String(), "\r\n", " ")
	cleaned = strings.NewReplacer("\r", " ", "\n", " ").Replace(cleaned)
	return strings.TrimSpace(cleaned)
}

func skipTerminalEscape(characters []rune, escapeIndex int) int {
	if escapeIndex+1 >= len(characters) {
		return escapeIndex
	}
	switch characters[escapeIndex+1] {
	case '[':
		index := escapeIndex + 2
		for index < len(characters) && (characters[index] < '@' || characters[index] > '~') {
			index++
		}
		return index
	case ']', 'P', '^', '_':
		for index := escapeIndex + 2; index < len(characters); index++ {
			if characters[index] == '\x07' {
				return index
			}
			if characters[index] == '\x1b' && index+1 < len(characters) && characters[index+1] == 0x5c {
				return index + 1
			}
		}
		return len(characters) - 1
	default:
		return escapeIndex + 1
	}
}
