package agent

import "testing"

func TestSanitizeInstallNameKeepsPathRulesSeparateFromTerminalCleaning(t *testing.T) {
	cases := map[string]string{
		"Mixed Case Skill":     "mixed-case-skill",
		"../../Outside Folder": "outside-folder",
		".hidden_skill":        "hidden_skill",
		"...":                  "unnamed-skill",
		"\x1b[31m My Skill":    "31m-my-skill",
	}
	for input, want := range cases {
		if got := SanitizeInstallName(input); got != want {
			t.Errorf("SanitizeInstallName(%q) = %q, want %q", input, got, want)
		}
	}
}
