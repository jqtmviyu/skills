package skillscan

import "testing"

func TestParseSourceRecognizesLocalAndGitSources(t *testing.T) {
	localDir := t.TempDir()
	cases := []struct {
		name        string
		input       string
		wantKind    SourceKind
		wantURL     string
		wantRef     string
		wantSubpath string
		wantFilter  string
	}{
		{name: "local directory", input: localDir, wantKind: LocalSource, wantURL: localDir},
		{name: "github shorthand", input: "vercel-labs/skills", wantKind: GitSource, wantURL: "https://github.com/vercel-labs/skills.git"},
		{name: "repository source alias", input: "vercel-labs/vercel-skills", wantKind: GitSource, wantURL: "https://github.com/vercel-labs/agent-skills.git"},
		{name: "github tree URL", input: "https://github.com/acme/repo/tree/feature-x/skills/alpha", wantKind: GitSource, wantURL: "https://github.com/acme/repo.git", wantRef: "feature-x", wantSubpath: "skills/alpha"},
		{name: "gitlab subgroup URL", input: "https://gitlab.com/group/team/repo.git/-/tree/main/plugins/one", wantKind: GitSource, wantURL: "https://gitlab.com/group/team/repo.git", wantRef: "main", wantSubpath: "plugins/one"},
		{name: "scp SSH URL", input: "git@git.example:team/repo.git", wantKind: GitSource, wantURL: "git@git.example:team/repo.git"},
		{name: "HTTPS ref fragment", input: "https://git.example/team/repo.git#release-2", wantKind: GitSource, wantURL: "https://git.example/team/repo.git", wantRef: "release-2"},
		{name: "GitHub skill selector", input: "vercel-labs/skills@find-skills", wantKind: GitSource, wantURL: "https://github.com/vercel-labs/skills.git", wantFilter: "find-skills"},
		{name: "GitHub prefix with subpath", input: "github:vercel-labs/skills/skills/alpha", wantKind: GitSource, wantURL: "https://github.com/vercel-labs/skills.git", wantSubpath: "skills/alpha"},
		{name: "URL ref and skill selector", input: "https://git.example/team/repo.git#release-2@docs", wantKind: GitSource, wantURL: "https://git.example/team/repo.git", wantRef: "release-2", wantFilter: "docs"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseSource(test.input)
			if err != nil {
				t.Fatalf("ParseSource(%q): %v", test.input, err)
			}
			if got.Kind != test.wantKind || got.URL != test.wantURL || got.Ref != test.wantRef || got.Subpath != test.wantSubpath || got.SkillFilter != test.wantFilter {
				t.Fatalf("ParseSource(%q) = %#v, want kind=%q url=%q ref=%q subpath=%q", test.input, got, test.wantKind, test.wantURL, test.wantRef, test.wantSubpath)
			}
		})
	}

}
