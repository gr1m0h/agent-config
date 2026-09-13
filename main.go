package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Config struct {
	Version int `json:"version"`
	Targets struct {
		Claude TargetConfig `json:"claude"`
		Codex  TargetConfig `json:"codex"`
	} `json:"targets"`
}

type TargetConfig struct {
	Output string `json:"output"`
}

type Manifest struct {
	Files []string `json:"files"`
}

type Generator struct {
	Root  string
	Force bool
	Dry   bool
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit(os.Args[2:])
	case "import-claude":
		err = cmdImportClaude(os.Args[2:])
	case "generate":
		err = cmdGenerate(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `agent-config: generate Claude Code and Codex config from one neutral source

Commands:
  init            create a neutral source tree
  import-claude   migrate an existing .claude directory into the neutral tree
  generate        generate claude, codex, or both
  check           validate source and print portability warnings

Run "agent-config <command> -h" for options.`)
}

func defaultConfig() Config {
	var c Config
	c.Version = 1
	c.Targets.Claude.Output = "~/.claude"
	c.Targets.Codex.Output = "~/.codex"
	return c
}

func cmdInit(args []string) error {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	root := f.String("root", ".", "neutral source directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	return initTree(*root)
}

func initTree(root string) error {
	dirs := []string{"skills", "agents", "rules", "hooks", "targets/claude", "targets/codex"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	if err := writeIfMissing(filepath.Join(root, "instructions.md"), []byte("# Agent instructions\n\nPut portable instructions here.\n"), 0o644); err != nil {
		return err
	}
	cfg, _ := json.MarshalIndent(defaultConfig(), "", "  ")
	cfg = append(cfg, '\n')
	if err := writeIfMissing(filepath.Join(root, "config.json"), cfg, 0o644); err != nil {
		return err
	}
	if err := writeIfMissing(filepath.Join(root, "targets/codex/config.toml"), []byte("# Codex-only settings.\n"), 0o644); err != nil {
		return err
	}
	if err := writeIfMissing(filepath.Join(root, "targets/claude/settings.json"), []byte("{}\n"), 0o644); err != nil {
		return err
	}
	fmt.Println("initialized", root)
	return nil
}

func cmdImportClaude(args []string) error {
	f := flag.NewFlagSet("import-claude", flag.ContinueOnError)
	root := f.String("root", ".", "neutral source directory")
	from := f.String("from", "~/.claude", "existing Claude Code config directory")
	force := f.Bool("force", false, "overwrite imported neutral files")
	if err := f.Parse(args); err != nil {
		return err
	}
	if err := initTree(*root); err != nil {
		return err
	}
	src, err := expandHome(*from)
	if err != nil {
		return err
	}

	mapping := [][2]string{
		{"CLAUDE.md", "instructions.md"},
		{"settings.json", "targets/claude/settings.json"},
		{"statusline.sh", "targets/claude/statusline.sh"},
	}
	for _, m := range mapping {
		s := filepath.Join(src, m[0])
		d := filepath.Join(*root, m[1])
		if _, err := os.Stat(s); err == nil {
			if err := copyFileGuarded(s, d, *force); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"skills", "agents", "rules", "hooks"} {
		s := filepath.Join(src, name)
		d := filepath.Join(*root, name)
		if st, err := os.Stat(s); err == nil && st.IsDir() {
			if err := copyTreeGuarded(s, d, *force); err != nil {
				return err
			}
		}
	}
	fmt.Printf("imported %s -> %s\n", src, *root)
	return nil
}

func cmdGenerate(args []string) error {
	f := flag.NewFlagSet("generate", flag.ContinueOnError)
	root := f.String("root", ".", "neutral source directory")
	target := f.String("target", "all", "claude, codex, or all")
	force := f.Bool("force", false, "overwrite colliding unmanaged files")
	dry := f.Bool("dry-run", false, "show writes without changing files")
	if err := f.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*root)
	if err != nil {
		return err
	}
	g := Generator{Root: *root, Force: *force, Dry: *dry}

	switch *target {
	case "claude":
		err = g.generateClaude(cfg.Targets.Claude.Output)
	case "codex":
		err = g.generateCodex(cfg.Targets.Codex.Output)
	case "all":
		if err = g.generateClaude(cfg.Targets.Claude.Output); err == nil {
			err = g.generateCodex(cfg.Targets.Codex.Output)
		}
	default:
		return fmt.Errorf("unknown target %q", *target)
	}
	if err != nil {
		return err
	}
	return nil
}

func cmdCheck(args []string) error {
	f := flag.NewFlagSet("check", flag.ContinueOnError)
	root := f.String("root", ".", "neutral source directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if _, err := loadConfig(*root); err != nil {
		return err
	}
	checks := []string{"instructions.md", "skills", "agents", "rules", "hooks", "targets/claude", "targets/codex"}
	for _, p := range checks {
		full := filepath.Join(*root, p)
		if _, err := os.Stat(full); err == nil {
			fmt.Println("✓", p)
		} else {
			fmt.Println("·", p, "(missing, optional except instructions.md)")
		}
	}
	if _, err := os.Stat(filepath.Join(*root, "instructions.md")); err != nil {
		return errors.New("instructions.md is required")
	}
	fmt.Println("\nPortability:")
	fmt.Println("✓ instructions: CLAUDE.md / AGENTS.md")
	fmt.Println("✓ skills: copied to both targets")
	fmt.Println("△ agents: native on Claude; lowered to Codex skills")
	fmt.Println("△ rules: native files on Claude; concatenated into Codex AGENTS.md")
	fmt.Println("△ hooks: native on Claude; scripts copied to Codex but not auto-registered")
	fmt.Println("△ statusline: Claude-only")
	fmt.Println("△ settings: target-specific overlays; no unsafe semantic translation")
	return nil
}

func loadConfig(root string) (Config, error) {
	b, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version != 1 {
		return c, fmt.Errorf("unsupported config version %d", c.Version)
	}
	return c, nil
}

func (g Generator) generateClaude(out string) error {
	dst, err := expandHome(out)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	instr, err := os.ReadFile(filepath.Join(g.Root, "instructions.md"))
	if err != nil {
		return err
	}
	files["CLAUDE.md"] = instr
	if err := collectTree(filepath.Join(g.Root, "skills"), "skills", files); err != nil {
		return err
	}
	if err := collectTree(filepath.Join(g.Root, "agents"), "agents", files); err != nil {
		return err
	}
	if err := collectTree(filepath.Join(g.Root, "rules"), "rules", files); err != nil {
		return err
	}
	if err := collectTree(filepath.Join(g.Root, "hooks"), "hooks", files); err != nil {
		return err
	}
	for _, p := range []string{"settings.json", "statusline.sh"} {
		s := filepath.Join(g.Root, "targets/claude", p)
		if b, err := os.ReadFile(s); err == nil {
			files[p] = b
		}
	}
	return g.writeTarget(dst, files, "claude")
}

func (g Generator) generateCodex(out string) error {
	dst, err := expandHome(out)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	agents, err := buildCodexAgentsMD(g.Root)
	if err != nil {
		return err
	}
	files["AGENTS.md"] = agents
	if err := collectTree(filepath.Join(g.Root, "skills"), "skills", files); err != nil {
		return err
	}

	// Claude-style agents have no reliable 1:1 local Codex file representation.
	// Lower them to portable Codex skills instead of pretending semantics match.
	agentDir := filepath.Join(g.Root, "agents")
	entries, _ := os.ReadDir(agentDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(agentDir, e.Name()))
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		skill := lowerAgentToSkill(name, string(b))
		files[filepath.ToSlash(filepath.Join("skills", "agents", sanitize(name), "SKILL.md"))] = []byte(skill)
	}

	// Keep hook implementations available for instructions/skills to call manually.
	if err := collectTree(filepath.Join(g.Root, "hooks"), "hooks", files); err != nil {
		return err
	}
	if b, err := os.ReadFile(filepath.Join(g.Root, "targets/codex/config.toml")); err == nil {
		files["config.toml"] = b
	}
	return g.writeTarget(dst, files, "codex")
}

func buildCodexAgentsMD(root string) ([]byte, error) {
	base, err := os.ReadFile(filepath.Join(root, "instructions.md"))
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	sb.WriteString("<!-- generated by agent-config; edit the neutral source, not this file -->\n\n")
	sb.Write(base)
	if len(base) > 0 && base[len(base)-1] != '\n' {
		sb.WriteByte('\n')
	}
	entries, _ := os.ReadDir(filepath.Join(root, "rules"))
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		sb.WriteString("\n# Additional portable rules\n")
	}
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(root, "rules", name))
		if err != nil {
			return nil, err
		}
		sb.WriteString("\n## " + strings.TrimSuffix(name, filepath.Ext(name)) + "\n\n")
		sb.Write(b)
		if len(b) > 0 && b[len(b)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}
	return []byte(sb.String()), nil
}

var descRe = regexp.MustCompile(`(?m)^description:\s*["']?([^\n"']+)["']?\s*$`)
var fmRe = regexp.MustCompile(`(?s)^---\s*\n.*?\n---\s*\n?`)

func lowerAgentToSkill(name, raw string) string {
	desc := "Use this imported agent workflow when its specialization matches the task."
	if m := descRe.FindStringSubmatch(raw); len(m) == 2 {
		desc = strings.TrimSpace(m[1])
	}
	body := fmRe.ReplaceAllString(raw, "")
	return fmt.Sprintf("---\nname: agent-%s\ndescription: %s\n---\n\n# Imported agent: %s\n\nThis skill was generated from the neutral agent definition. Claude-specific model, permission, and tool-selection metadata is not assumed to be portable.\n\n%s", sanitize(name), yamlScalar(desc), name, body)
}

func yamlScalar(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9._-]+`)
	s = strings.Trim(re.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "agent"
	}
	return s
}

func collectTree(srcRoot, prefix string, files map[string][]byte) error {
	st, err := os.Stat(srcRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(filepath.Join(prefix, rel))] = b
		return nil
	})
}

func (g Generator) writeTarget(dst string, files map[string][]byte, target string) error {
	manifestPath := filepath.Join(dst, ".agent-config-manifest.json")
	old := Manifest{}
	if b, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	managed := map[string]bool{}
	for _, p := range old.Files {
		managed[p] = true
	}

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		path := filepath.Join(dst, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil && !managed[rel] && !g.Force {
			return fmt.Errorf("refusing to overwrite unmanaged file %s (use --force once to adopt it)", path)
		}
		if g.Dry {
			fmt.Printf("DRY write %s\n", path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(path, files[rel], mode); err != nil {
			return err
		}
	}
	if !g.Dry {
		m := Manifest{Files: keys}
		b, _ := json.MarshalIndent(m, "", "  ")
		b = append(b, '\n')
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, b, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("generated %s -> %s (%d files)\n", target, dst, len(files))
	if target == "codex" {
		fmt.Println("warning: Codex hooks are copied but not lifecycle-registered; neutral agents are lowered to skills")
	}
	return nil
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return h, nil
		}
		return filepath.Join(h, p[2:]), nil
	}
	return filepath.Abs(p)
}

func writeIfMissing(path string, b []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, mode)
}

func copyFileGuarded(src, dst string, force bool) error {
	if _, err := os.Stat(dst); err == nil && !force {
		return fmt.Errorf("destination exists: %s (use --force)", dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}

func copyTreeGuarded(src, dst string, force bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFileGuarded(path, target, force)
	})
}
