package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentSkillMatchesRepoRoot pins the embedded skill to the canonical
// codetwin-SKILL.md at the repo root: edit one without the other and
// this fails, so agent-install can never ship a stale skill.
func TestAgentSkillMatchesRepoRoot(t *testing.T) {
	root, err := os.ReadFile("../../codetwin-SKILL.md")
	if err != nil {
		t.Fatalf("read repo-root skill: %v", err)
	}
	if string(root) != agentSkillBody {
		t.Fatalf("cmd/codetwin/agent_skill.md differs from codetwin-SKILL.md; copy the repo-root file over the embedded one")
	}
}

func TestParseAgentSkillFrontmatter(t *testing.T) {
	p := parseAgentSkill(agentSkillBody)
	if p.description == "" {
		t.Fatalf("skill frontmatter description should parse non-empty")
	}
	if !strings.Contains(p.description, "dead") {
		t.Errorf("skill description should mention dead code triggers, got: %s", p.description)
	}
	if strings.HasPrefix(p.body, "---") {
		t.Errorf("body should not retain frontmatter:\n%.80s", p.body)
	}
	if !strings.HasPrefix(p.body, "# codetwin Skill") {
		t.Errorf("body should start at the markdown heading, got:\n%.80s", p.body)
	}
}

func TestAgentInstallDedicated(t *testing.T) {
	dir := t.TempDir()

	if err := agentInstall("claude", "project", dir, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	dest := filepath.Join(dir, ".claude/skills/codetwin/SKILL.md")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(got) != agentSkillBody {
		t.Errorf("claude target should receive the verbatim skill file")
	}

	// Re-run: unchanged, no error.
	if err := agentInstall("claude", "project", dir, false, false); err != nil {
		t.Fatalf("re-install should be a no-op: %v", err)
	}

	// Differing existing content: refused without --force, replaced with.
	if err := os.WriteFile(dest, []byte("user edits"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agentInstall("claude", "project", dir, false, false); err == nil {
		t.Fatalf("differing file must be refused without --force")
	}
	if err := agentInstall("claude", "project", dir, true, false); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
	got, _ = os.ReadFile(dest)
	if string(got) != agentSkillBody {
		t.Errorf("--force should restore the skill content")
	}
}

func TestAgentInstallCursorRendersMDC(t *testing.T) {
	dir := t.TempDir()
	if err := agentInstall("cursor", "project", dir, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".cursor/rules/codetwin.mdc"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "---\ndescription: ") || !strings.Contains(s, "alwaysApply: false") {
		t.Errorf("cursor rule should carry .mdc frontmatter:\n%.120s", s)
	}
}

func TestAgentInstallSharedBlockLifecycle(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(dest, []byte("# Existing instructions\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install appends a marked block, preserving existing content.
	if err := agentInstall("agents", "project", dir, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, _ := os.ReadFile(dest)
	s := string(got)
	if !strings.Contains(s, "keep me") {
		t.Errorf("existing content must be preserved:\n%s", s)
	}
	if !strings.Contains(s, agentBlockBegin) || !strings.Contains(s, agentBlockEnd) {
		t.Errorf("managed block markers missing:\n%s", s)
	}

	// Re-run: idempotent.
	if err := agentInstall("agents", "project", dir, false, false); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	got2, _ := os.ReadFile(dest)
	if string(got2) != s {
		t.Errorf("re-install must not change the file")
	}
	if strings.Count(string(got2), agentBlockBegin) != 1 {
		t.Errorf("managed block must not duplicate")
	}
}

func TestAgentInstallRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	if err := agentInstall("nope", "project", dir, false, false); err == nil {
		t.Errorf("unknown agent must error")
	}
	if err := agentInstall("claude", "galaxy", dir, false, false); err == nil {
		t.Errorf("invalid scope must error")
	}
	if err := agentInstall("cursor", "user", dir, false, false); err == nil {
		t.Errorf("cursor has no user scope; must error")
	}
}

// TestAgentInstallSubcommandDispatch drives the real binary: the
// subcommand must bypass scan-flag parsing entirely.
func TestAgentInstallSubcommandDispatch(t *testing.T) {
	bin := subprocessBin(t)
	dir := t.TempDir()

	out, err := exec.Command(bin, "agent-install", "claude", "--scope", "project", "--dir", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("subcommand run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "installed claude skill") {
		t.Errorf("expected install confirmation, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/skills/codetwin/SKILL.md")); err != nil {
		t.Errorf("skill file not written: %v", err)
	}

	out, err = exec.Command(bin, "agent-install", "--list").CombinedOutput()
	if err != nil {
		t.Fatalf("--list: %v\n%s", err, out)
	}
	for _, id := range []string{"claude", "cursor", "windsurf", "cline", "copilot", "agents"} {
		if !strings.Contains(string(out), id) {
			t.Errorf("--list should mention %s:\n%s", id, out)
		}
	}
}
