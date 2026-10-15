package skillscan

import (
	"context"
	"testing"
)

func TestSkillMetadataStripsTerminalControlSequences(t *testing.T) {
	repo := t.TempDir()
	content := `---
name: "\x1b[31mred\x1b[0m name"
description: "clear\x1b]0;untrusted-title\x07 text"
---
Body.
`
	writeSkillFile(t, repo, "skills/terminal/SKILL.md", content)

	scan, err := Open(context.Background(), repo, Options{})
	if err != nil {
		t.Fatalf("Open local source: %v", err)
	}
	defer scan.Close()
	candidates := scan.Candidates()
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v, want one", candidates)
	}
	if candidates[0].Name != "red name" || candidates[0].Description != "clear text" {
		t.Fatalf("sanitized metadata = (%q, %q)", candidates[0].Name, candidates[0].Description)
	}
}
