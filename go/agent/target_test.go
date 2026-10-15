package agent

import (
	"path/filepath"
	"testing"
)

func TestResolveTargetUsesProjectGlobalAndUniversalDirectories(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-profile"))

	cases := []struct {
		name          string
		id            ID
		global        bool
		wantCanonical string
		wantAgent     string
	}{
		{"agent-specific project", "claude-code", false, filepath.Join(project, ".agents", "skills"), filepath.Join(project, ".claude", "skills")},
		{"universal project", "codex", false, filepath.Join(project, ".agents", "skills"), filepath.Join(project, ".agents", "skills")},
		{"agent-specific global override", "claude-code", true, filepath.Join(home, ".agents", "skills"), filepath.Join(home, "claude-profile", "skills")},
		{"universal global uses canonical path", "cursor", true, filepath.Join(home, ".agents", "skills"), filepath.Join(home, ".agents", "skills")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			target, err := ResolveTarget(test.id, "My Skill", TargetOptions{Global: test.global, CWD: project})
			if err != nil {
				t.Fatalf("resolve target: %v", err)
			}
			if target.CanonicalBase != test.wantCanonical {
				t.Errorf("canonical base = %q, want %q", target.CanonicalBase, test.wantCanonical)
			}
			if target.AgentBase != test.wantAgent {
				t.Errorf("agent base = %q, want %q", target.AgentBase, test.wantAgent)
			}
			if target.InstallName != "my-skill" {
				t.Errorf("install name = %q, want my-skill", target.InstallName)
			}
			if target.CanonicalDir != filepath.Join(test.wantCanonical, "my-skill") {
				t.Errorf("canonical dir = %q", target.CanonicalDir)
			}
			if target.AgentDir != filepath.Join(test.wantAgent, "my-skill") {
				t.Errorf("agent dir = %q", target.AgentDir)
			}
		})
	}
}

func TestResolveTargetPreservesEveRootAndSubagentPlacement(t *testing.T) {
	project := t.TempDir()
	target, err := ResolveTarget("eve", "writer skill", TargetOptions{CWD: project, EveSubagent: "../../Research"})
	if err != nil {
		t.Fatalf("resolve Eve subagent: %v", err)
	}
	want := filepath.Join(project, "agent", "subagents", "research", "skills")
	if target.CanonicalBase != want || target.AgentBase != want {
		t.Fatalf("Eve subagent bases = %q / %q, want %q", target.CanonicalBase, target.AgentBase, want)
	}
	rootTarget, err := ResolveTarget("eve", "root-skill", TargetOptions{CWD: project})
	if err != nil {
		t.Fatalf("resolve Eve root: %v", err)
	}
	if want := filepath.Join(project, "agent", "skills"); rootTarget.AgentBase != want || rootTarget.CanonicalBase != want {
		t.Fatalf("Eve root bases = %q / %q, want %q", rootTarget.CanonicalBase, rootTarget.AgentBase, want)
	}
}

func TestResolveTargetRejectsUnsupportedGlobalAgentsAndUnknownIDs(t *testing.T) {
	for _, id := range []ID{"eve", "promptscript"} {
		if _, err := ResolveTarget(id, "skill", TargetOptions{Global: true}); err == nil {
			t.Errorf("global target for %q succeeded", id)
		}
	}
	if _, err := ResolveTarget("not-an-agent", "skill", TargetOptions{}); err == nil {
		t.Fatal("unknown agent target succeeded")
	}
}

func TestResolveTargetUsesHomeAndXDGEnvironment(t *testing.T) {
	home := t.TempDir()
	config := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	target, err := ResolveTarget("universal", "skill", TargetOptions{Global: true, CWD: project})
	if err != nil {
		t.Fatalf("resolve universal pseudo-target: %v", err)
	}
	if want := filepath.Join(home, ".agents", "skills"); target.AgentBase != want {
		t.Fatalf("universal install dir = %q, want %q", target.AgentBase, want)
	}
	if target.AgentBase == filepath.Join(config, "agents", "skills") {
		t.Fatal("universal target used the XDG registry path instead of canonical install directory")
	}
}

func TestResolveTargetCoversEveryRegistryProjectAndGlobalDestination(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, key := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR", "VIBE_HOME", "HERMES_HOME", "AUTOHAND_HOME", "GROK_HOME"} {
		t.Setenv(key, "")
	}
	for _, config := range Registry() {
		t.Run(string(config.ID), func(t *testing.T) {
			projectTarget, err := ResolveTarget(config.ID, "skill", TargetOptions{CWD: project})
			if err != nil {
				t.Fatalf("resolve project target: %v", err)
			}
			wantProjectBase := filepath.Join(project, filepath.FromSlash(config.SkillsDir))
			if config.SkillsDir == UniversalSkillsDir {
				wantProjectBase = filepath.Join(project, ".agents", "skills")
			}
			if projectTarget.AgentBase != wantProjectBase {
				t.Errorf("project agent base = %q, want %q", projectTarget.AgentBase, wantProjectBase)
			}
			if !config.SupportsGlobal {
				if _, err := ResolveTarget(config.ID, "skill", TargetOptions{Global: true, CWD: project}); err == nil {
					t.Error("unsupported global target resolved successfully")
				}
				return
			}
			globalTarget, err := ResolveTarget(config.ID, "skill", TargetOptions{Global: true, CWD: project})
			if err != nil {
				t.Fatalf("resolve global target: %v", err)
			}
			wantGlobalBase := config.GlobalSkillsDir
			if config.SkillsDir == UniversalSkillsDir {
				wantGlobalBase = filepath.Join(home, ".agents", "skills")
			}
			if globalTarget.AgentBase != wantGlobalBase {
				t.Errorf("global agent base = %q, want %q", globalTarget.AgentBase, wantGlobalBase)
			}
		})
	}
}

func TestResolveTargetAppliesProjectAgentRootPolicy(t *testing.T) {
	project := t.TempDir()
	target, err := ResolveTarget("windsurf", "skill", TargetOptions{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if !target.SkipAgentLink || target.SkipReason != MissingAgentProjectDirectory || target.InstallDir != target.CanonicalDir {
		t.Fatalf("missing agent root target = %#v", target)
	}

	selected, err := ResolveTarget("windsurf", "skill", TargetOptions{CWD: project, CreateMissingAgentRoot: true})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SkipAgentLink {
		t.Fatal("explicitly selected agent did not permit creating its project root")
	}

	claude, err := ResolveTarget("claude-code", "skill", TargetOptions{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if claude.SkipAgentLink {
		t.Fatal("Claude Code compatibility policy should create a missing project root")
	}

	eve, err := ResolveTarget("eve", "skill", TargetOptions{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if eve.SkipAgentLink {
		t.Fatal("Eve's canonical and agent paths coincide, so no agent link should be skipped")
	}
}
