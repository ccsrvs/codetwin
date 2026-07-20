package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// agentSkillBody is the generic codetwin skill, embedded so agent-install
// can write it into any supported harness without needing the repo
// checkout. Kept byte-identical to codetwin-SKILL.md at the repo root
// (asserted by a test).
//
//go:embed agent_skill.md
var agentSkillBody string

// agentTarget describes where and how one harness wants the skill.
type agentTarget struct {
	id    string
	label string
	// relPaths maps an install scope to the destination path relative to
	// that scope's base directory (cwd for "project", $HOME for "user").
	// A scope absent from the map is unsupported for this harness.
	relPaths map[string]string
	// render produces the file contents for this harness from the skill.
	render func(agentSkillParts) string
	// shared marks files that other tools also write to (AGENTS.md,
	// copilot-instructions.md): those get an idempotent marked block
	// spliced in rather than a whole-file overwrite.
	shared bool
}

var agentTargets = []agentTarget{
	{
		id:    "claude",
		label: "Claude Code skill",
		relPaths: map[string]string{
			"project": ".claude/skills/codetwin/SKILL.md",
			"user":    ".claude/skills/codetwin/SKILL.md",
		},
		render: func(p agentSkillParts) string { return p.full },
	},
	{
		id:       "cursor",
		label:    "Cursor project rule",
		relPaths: map[string]string{"project": ".cursor/rules/codetwin.mdc"},
		render: func(p agentSkillParts) string {
			return "---\ndescription: " + p.description + "\nalwaysApply: false\n---\n\n" + p.body
		},
	},
	{
		id:       "windsurf",
		label:    "Windsurf project rule",
		relPaths: map[string]string{"project": ".windsurf/rules/codetwin.md"},
		render:   func(p agentSkillParts) string { return p.body },
	},
	{
		id:       "cline",
		label:    "Cline project rule",
		relPaths: map[string]string{"project": ".clinerules/codetwin.md"},
		render:   func(p agentSkillParts) string { return p.body },
	},
	{
		id:       "copilot",
		label:    "GitHub Copilot repo instructions",
		relPaths: map[string]string{"project": ".github/copilot-instructions.md"},
		render:   func(p agentSkillParts) string { return p.body },
		shared:   true,
	},
	{
		id:       "agents",
		label:    "AGENTS.md (Codex & compatible)",
		relPaths: map[string]string{"project": "AGENTS.md"},
		render:   func(p agentSkillParts) string { return p.body },
		shared:   true,
	},
}

const (
	agentBlockBegin = "<!-- BEGIN codetwin skill (managed by `codetwin agent-install`) -->"
	agentBlockEnd   = "<!-- END codetwin skill -->"
)

// agentSkillParts is the embedded skill split into frontmatter fields
// and body: harnesses with their own metadata format get the body and
// re-wrap it; Claude Code takes the file verbatim.
type agentSkillParts struct {
	description string
	body        string
	full        string
}

func parseAgentSkill(s string) agentSkillParts {
	p := agentSkillParts{full: s, body: s}
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return p
	}
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return p
	}
	front := rest[:end]
	p.body = strings.TrimLeft(rest[end+5:], "\n")
	// description may be a folded multi-line scalar ("description: >");
	// gather its continuation lines into one space-joined string.
	var desc []string
	inDesc := false
	for _, line := range strings.Split(front, "\n") {
		switch {
		case strings.HasPrefix(line, "description:"):
			inDesc = true
			v := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			if v != "" && v != ">" && v != "|" {
				desc = append(desc, v)
			}
		case inDesc && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")):
			desc = append(desc, strings.TrimSpace(line))
		default:
			inDesc = false
		}
	}
	p.description = strings.Join(desc, " ")
	return p
}

// runAgentInstallCLI is the `codetwin agent-install` entry point,
// dispatched from main before the scan flags parse. Exits the process.
func runAgentInstallCLI(args []string) {
	fs := flag.NewFlagSet("agent-install", flag.ExitOnError)
	scope := fs.String("scope", "project", "install scope: project | user")
	dir := fs.String("dir", "", "base directory override (default: cwd for project, $HOME for user)")
	force := fs.Bool("force", false, "overwrite an existing dedicated file that differs")
	list := fs.Bool("list", false, "list supported agents and their target paths")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `
codetwin agent-install — wire codetwin into a coding agent

USAGE:
  codetwin agent-install <agent> [--scope project|user] [--dir <base>] [--force] [--json]
  codetwin agent-install --list

Writes the generic codetwin skill into the agent's config so it knows
when and how to drive this CLI. Re-run after upgrading codetwin to pick
up skill changes. Most agents are project-scoped; claude also supports
--scope user (all your projects).

`)
		fs.PrintDefaults()
	}
	// Stdlib flag parsing stops at the first positional argument, so the
	// documented `agent-install <agent> [flags]` order would leave every
	// flag unparsed. Pull a leading non-flag token off as the agent id
	// first; `agent-install [flags] <agent>` then also works for free.
	agentID := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agentID, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if agentID == "" && fs.NArg() > 0 {
		agentID = fs.Arg(0)
	}
	if *list || agentID == "" {
		listAgentTargets(*jsonOut)
		return
	}
	if err := agentInstall(agentID, *scope, *dir, *force, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func agentInstall(agentID, scope, dir string, force, jsonOut bool) error {
	scope = strings.ToLower(scope)
	if scope != "project" && scope != "user" {
		return fmt.Errorf("invalid --scope %q: use \"project\" or \"user\"", scope)
	}
	var agent agentTarget
	found := false
	for _, a := range agentTargets {
		if a.id == strings.ToLower(agentID) {
			agent, found = a, true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown agent %q; run `codetwin agent-install --list`", agentID)
	}
	rel, ok := agent.relPaths[scope]
	if !ok {
		scopes := make([]string, 0, len(agent.relPaths))
		for s := range agent.relPaths {
			scopes = append(scopes, s)
		}
		sort.Strings(scopes)
		return fmt.Errorf("agent %q does not support --scope %s (supported: %s)",
			agent.id, scope, strings.Join(scopes, ", "))
	}

	base := dir
	if base == "" {
		var err error
		if scope == "user" {
			base, err = os.UserHomeDir()
		} else {
			base, err = os.Getwd()
		}
		if err != nil {
			return err
		}
	}
	dest := filepath.Join(base, filepath.FromSlash(rel))
	content := agent.render(parseAgentSkill(agentSkillBody))

	var action string
	var err error
	if agent.shared {
		action, err = spliceManagedBlock(dest, content)
	} else {
		action, err = writeDedicatedFile(dest, content, force)
	}
	if err != nil {
		return err
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		return enc.Encode(map[string]string{
			"agent": agent.id, "scope": scope, "path": dest, "action": action,
		})
	}
	fmt.Printf("%s %s skill (%s scope)\n  %s\n", action, agent.id, scope, dest)
	if action != "unchanged" && !agent.shared {
		fmt.Println("The agent will pick it up on its next session.")
	}
	return nil
}

// writeDedicatedFile writes a file this skill owns exclusively,
// refusing to clobber differing content unless force is set.
func writeDedicatedFile(dest, content string, force bool) (string, error) {
	if existing, err := os.ReadFile(dest); err == nil {
		if string(existing) == content {
			return "unchanged", nil
		}
		if !force {
			return "", fmt.Errorf("%s already exists and differs; pass --force to overwrite", dest)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "installed", nil
}

// spliceManagedBlock inserts or replaces codetwin's marked block in a
// file other tools may also own, preserving surrounding content.
func spliceManagedBlock(dest, body string) (string, error) {
	block := agentBlockBegin + "\n" + body + "\n" + agentBlockEnd + "\n"

	existing, err := os.ReadFile(dest)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	old := string(existing)

	if i := strings.Index(old, agentBlockBegin); i >= 0 {
		j := strings.Index(old, agentBlockEnd)
		if j < i {
			return "", fmt.Errorf("%s has a malformed codetwin block; fix or remove it and retry", dest)
		}
		updated := old[:i] + strings.TrimSuffix(block, "\n") + old[j+len(agentBlockEnd):]
		if updated == old {
			return "unchanged", nil
		}
		return "updated", os.WriteFile(dest, []byte(updated), 0o644)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var out string
	switch {
	case old == "":
		out = block
	case strings.HasSuffix(old, "\n\n"):
		out = old + block
	case strings.HasSuffix(old, "\n"):
		out = old + "\n" + block
	default:
		out = old + "\n\n" + block
	}
	if err := os.WriteFile(dest, []byte(out), 0o644); err != nil {
		return "", err
	}
	if old == "" {
		return "installed", nil
	}
	return "updated", nil
}

func listAgentTargets(jsonOut bool) {
	if jsonOut {
		agents := make([]map[string]any, 0, len(agentTargets))
		for _, a := range agentTargets {
			agents = append(agents, map[string]any{
				"id": a.id, "label": a.label, "scopes": a.relPaths,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"agents": agents})
		return
	}
	fmt.Println("Supported agents (codetwin agent-install <agent> [--scope project|user]):")
	fmt.Println()
	for _, a := range agentTargets {
		fmt.Printf("  %-9s %s\n", a.id, a.label)
		fmt.Printf("            project: %s\n", a.relPaths["project"])
		if u, ok := a.relPaths["user"]; ok {
			fmt.Printf("            user:    ~/%s\n", u)
		}
	}
}
