# devctx

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/hiroyannnn/devctx?include_prereleases)](https://github.com/hiroyannnn/devctx/releases)

[日本語](README.ja.md)

A CLI tool for managing Claude Code sessions and git worktrees in a kanban-style interface.

![devctx list](assets/list.gif)

## Features

- **Kanban View** - Visualize session states at a glance
- **Auto Session Tracking** - Automatic registration via Claude Code hooks
- **Status Management** - in-progress / review / blocked / done
- **Checklists** - Confirmation items during status transitions
- **Shell Integration** - One-command context switching
- **fzf Integration** - Interactive context selection
- **TUI Dashboard** - Interactive UI powered by Bubble Tea
- **Time Tracking** - Accumulated work time per session
- **Notes** - Add memos to contexts
- **GitHub Integration** - Auto-detect and link Issues/PRs
- **Worktree Creation** - Create branch to Claude launch in one command

## Installation

```bash
go install github.com/hiroyannnn/devctx@latest
```

Or build from source:

```bash
git clone https://github.com/hiroyannnn/devctx.git
cd devctx
go build -o devctx .
mv devctx ~/.local/bin/  # or /usr/local/bin/
```

## Quick Start

```bash
# Set up Claude Code hooks
devctx hooks --install

# Enable shell integration (add to .bashrc / .zshrc)
eval "$(devctx shell-init)"

# Display kanban view
devctx list
```

## Kanban View Example

```
🚀 In Progress
╭──────────────────────────────────────────────╮
│ [auth]                                       │
│   💬 zesty-hopping-falcon                    │
│   ⎇ feature/auth                             │
│   ⏱ 2h ago  ⌛ 4h32m                         │
│   📝 Working on OAuth2 refresh tokens        │
╰──────────────────────────────────────────────╯

👀 Review
╭──────────────────────────────────────────────╮
│ [api-fix]                                    │
│   💬 playful-coding-knuth                    │
│   ⎇ fix/api-error                            │
│   ⏱ 30m ago                                  │
│   🔀 https://github.com/user/repo/pull/123   │
│   ☑ /compact                                 │
│   ☐ /create-pr                               │
╰──────────────────────────────────────────────╯
```

💬 shows Claude Code's auto-generated session name (slug).

## Commands

### Basic Operations

| Command | Description |
|---------|-------------|
| `devctx list` | Display contexts in kanban view |
| `devctx tui` | Open interactive TUI dashboard |
| `devctx show <name>` | Show context details |
| `devctx register <name>` | Register a context (usually auto via hook) |
| `devctx resume <name>` | Resume a context |
| `devctx move <name> <status>` | Change status |
| `devctx archive <name>` | Archive as done |

### Creation & Configuration

| Command | Description |
|---------|-------------|
| `devctx new <branch>` | Create worktree + cd + claude in one go |
| `devctx note <name> [msg]` | Add/show a note |
| `devctx link <name> <url>` | Link GitHub Issue/PR |
| `devctx hooks [--install]` | Set up Claude Code hooks |
| `devctx commands [--install]` | Set up Claude slash commands |

### GitHub Integration

| Command | Description |
|---------|-------------|
| `devctx sync [name]` | Auto-detect and link PR/Issue |
| `devctx sync --all` | Update session names for all contexts |
| `devctx pr <name>` | Create a PR |

### Monitoring & Search

| Command | Description |
|---------|-------------|
| `devctx discover` | Find existing Claude Code sessions |
| `devctx discover --import` | Import discovered sessions |
| `devctx status` | Show live status of all contexts |
| `devctx status --watch` | Continuously monitor status |
| `devctx search <query>` | Search through session history |

### Maintenance

| Command | Description |
|---------|-------------|
| `devctx stats` | Show statistics |
| `devctx clean` | Remove old contexts (default: done > 30 days) |
| `devctx clean --days=7` | Remove contexts older than 7 days |
| `devctx clean --done=false` | Remove old contexts regardless of status |
| `devctx clean --dry-run` | Preview what would be removed |

## Shell Integration

Add to `.bashrc` or `.zshrc`:

```bash
eval "$(devctx shell-init)"
```

Shortcuts:
- `dx` - Select context via fzf and resume (shows list if fzf not available)
- `dx <name>` - Resume context (cd + claude --resume)
- `dx -` - Resume last touched context
- `dxl` - List contexts
- `dxw` - Watch mode (interactive kanban)
- `dxm <name> <status>` - Change status
- `dxn <branch>` - Create new worktree
- `dxs` - Sync GitHub info
- `dxt` - Open TUI dashboard
- `dxp` - Show live status
- `dxf <query>` - Search session history
- `dxd` - Discover existing sessions

## Watch Mode

Interactive kanban view with keyboard controls:

```bash
devctx list -w   # or dxw
```

![devctx watch mode](assets/watch.gif)

**Navigation:**
- `↑`/`↓` or `j`/`k` - Move cursor
- `g`/`G` - Jump to top/bottom

**Actions:**
- `Enter` or `c` - Copy resume command to clipboard
- `o` - Open in new terminal

**Status Changes:**
- `r` - Move to Review
- `p` - Move to In Progress
- `b` - Move to Blocked
- `D` - Move to Done
- `x` - Delete context
- `q` - Quit

## Configuration

Config file: `~/.config/devctx/config.yaml`

```yaml
# Show completed items for N days (default: 1)
done_retention_days: 1

# Disable auto-import of sessions (default: true)
auto_import: false

statuses:
  - name: in-progress
    next: [review, blocked, done]
  - name: review
    next: [in-progress, done]
    checklist:
      - /compact
  - name: blocked
    next: [in-progress]
  - name: done
    next: []
    archive: true
    checklist:
      - /create-pr
```

### Customizing Checklists

Add items to confirm during status transitions:

```yaml
statuses:
  - name: review
    next: [in-progress, done]
    checklist:
      - /compact
      - /code-simplifier
      - "PR draft created?"
```

## Claude Code Custom Commands

Install slash commands for Claude Code:

```bash
devctx commands --install
```

This creates:
- `/devctx-review` - Move to review status
- `/devctx-done` - Mark as done
- `/devctx-blocked` - Mark as blocked
- `/devctx-note` - Add a note
- `/devctx-link` - Link Issue/PR
- `/devctx-status` - Show context status

## Troubleshooting

### Hooks not working

1. Check settings with `devctx hooks`
2. Run `/hooks` in Claude Code to approve
3. Ensure `devctx` is in PATH

### Session not auto-registered

- Verify `SessionStart` hook is properly configured
- Test manually with `devctx register`

### resume doesn't change directory

Due to shell constraints, subprocesses cannot change the parent shell's directory.
Use shell integration (`eval "$(devctx shell-init)"`) or manually execute the displayed commands.

## License

MIT
