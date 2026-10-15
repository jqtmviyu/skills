package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenClawGlobalSkillsDirectoryFollowsExistingConfigRoot(t *testing.T) {
	for _, test := range []struct {
		name   string
		marker string
		want   string
	}{
		{"default", "", ".openclaw"},
		{"legacy-clawdbot", ".clawdbot", ".clawdbot"},
		{"legacy-moltbot", ".moltbot", ".moltbot"},
		{"current-config-wins", ".openclaw", ".openclaw"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if test.marker != "" {
				if err := os.Mkdir(filepath.Join(home, test.marker), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			config, ok := ConfigFor("openclaw")
			want := filepath.Join(home, test.want, "skills")
			if !ok || config.GlobalSkillsDir != want {
				t.Fatalf("OpenClaw directory = %q, supported = %v, want %q", config.GlobalSkillsDir, ok, want)
			}
		})
	}
}
