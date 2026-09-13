# agent-config

A small, one-way configuration compiler for people who use both Claude Code and Codex.

The important design decision is that **neither `.claude/` nor `.codex/` is the source of truth**. A neutral directory is edited by hand; tool-native directories are generated.

## Neutral layout

```text
agents-config/
├── config.json
├── instructions.md
├── skills/
├── agents/
├── rules/
├── hooks/
└── targets/
    ├── claude/
    │   ├── settings.json
    │   └── statusline.sh
    └── codex/
        └── config.toml
```

Portable content lives at the top level. Vendor-specific settings live under `targets/` instead of being forced into a lossy common schema.

## Mapping

| Neutral source | Claude Code | Codex |
|---|---|---|
| `instructions.md` | `CLAUDE.md` | `AGENTS.md` |
| `skills/` | `skills/` | `skills/` |
| `agents/*.md` | `agents/*.md` | lowered to `skills/agents/*/SKILL.md` |
| `rules/*.md` | `rules/*.md` | appended to `AGENTS.md` |
| `hooks/` | `hooks/` | scripts copied to `hooks/`, not lifecycle-registered |
| `targets/claude/settings.json` | `settings.json` | — |
| `targets/claude/statusline.sh` | `statusline.sh` | — |
| `targets/codex/config.toml` | — | `config.toml` |

The generator intentionally does **not** claim Claude hooks, permission settings, model names, or subagent metadata are semantically equivalent to Codex concepts.

## Build

```bash
go build -o agent-config .
```

## Start fresh

```bash
mkdir -p ~/.config/agents-config
agent-config init --root ~/.config/agents-config
```

## Migrate an existing `~/.claude`

```bash
agent-config import-claude \
  --from ~/.claude \
  --root ~/.config/agents-config \
  --force
```

Review the imported files, then stop editing generated `~/.claude` / `~/.codex` files directly.

## Generate

```bash
agent-config check --root ~/.config/agents-config

agent-config generate \
  --root ~/.config/agents-config \
  --target all \
  --force
```

After the first generation, a manifest tracks managed files, so subsequent runs do not require `--force` for those files. Unmanaged collisions fail closed.

Use `--dry-run` before adopting existing target directories:

```bash
agent-config generate --root ~/.config/agents-config --target all --dry-run
```

## Dotfiles recommendation

Track the neutral source in dotfiles:

```text
dotfiles/
└── .config/agents-config/
    ├── instructions.md
    ├── skills/
    ├── agents/
    ├── rules/
    ├── hooks/
    └── targets/
```

Treat `.claude/` and `.codex/` as generated deployment outputs.

For a work machine that only uses Claude, run `generate --target claude`. For a personal machine, use `--target all`.

## Current deliberate limitations

- Codex lifecycle hooks are **not fabricated** from Claude hook events. Hook scripts are copied so skills/instructions can invoke them, but registration is target-specific.
- Claude custom agents are lowered to Codex skills. Claude-only frontmatter such as model/tool/permission choices is intentionally removed.
- `settings.json` and `config.toml` are target overlays, not automatically translated. Permission/sandbox semantics differ enough that guessing would be unsafe.
- Project-local hierarchical rules could later be compiled into nested `AGENTS.md` files; v1 only handles the home/global configuration use case.
