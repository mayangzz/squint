# squint

**Squint at your coding agent.** Every tool call becomes one live status line. The agent's answers stay exactly as they are.

![Stock Claude Code on the left, the same session through squint on the right](docs/demo.gif)

```bash
brew install --cask mayangzz/tap/squint      # or: go install github.com/mayangzz/squint@latest
sq() { squint --relay claude "$@"; }         # add to ~/.zshrc, then run `sq` instead of `claude`
```

[中文说明](README.zh-CN.md)

---

## Why

A coding agent narrates every move: the full shell command, a preview of its output, `… +78 lines (ctrl+o to expand)`, `Allowed by auto mode classifier`. You scroll past all of it looking for the two sentences that answer your question.

squint folds each step into a single line that updates in place:

```
🔍 running  go test ./... ×3
📖 reading  internal/relay/filter.go
✏️  editing  service/bill/payload.go
```

You can still see what it's doing and that it hasn't stalled. You just don't have to read the transcript of a shell session.

**It changes nothing about how the agent runs.** Same permissions, slash commands, Esc-to-interrupt, image paste, mouse selection, scrollback. Only the display changes. Stop using the wrapper and nothing needs undoing.

### How is this different from Claude Code's own views?

| | Steps | Answers |
|---|---|---|
| Stock view | every call with output previews | ✓ |
| Focus-style views | hidden | final message only |
| **squint** | **one live line per step** | **all of them** |

Recent Claude Code versions already group searches and file reads (`Searched for 4 patterns`). squint also folds everything they still print in full, like shell commands with output previews, permission notes and edits, and merges runs of the same tool into one counter.

---

## Install

| | |
|---|---|
| Homebrew (macOS / Linux) | `brew install --cask mayangzz/tap/squint` |
| Binary | download from [Releases](https://github.com/mayangzz/squint/releases), put `squint` on your `PATH` |
| Go 1.25+ | `go install github.com/mayangzz/squint@latest` |

## Use

```bash
# wrap the agent, keep plain `claude` next to it for A/B
sq() { squint --relay claude "$@"; }

sq                      # a normal interactive session, quieter
sq --resume             # extra arguments pass straight through
squint --check          # verify the fold rules still match your agent version
```

Prefer to replace `claude` outright? `alias claude='squint --relay command claude'`.

Headless one-shots work too: `squint "explain this repo"`, `squint --lens errors "fix the failing test"`, `squint --replay run.jsonl`. See [docs/customize.md](docs/customize.md).

## Supported agents

| Agent | Interactive (`--relay`) | Headless |
|---|---|---|
| **Claude Code** | ✅ tested on 2.1.227 and 2.1.284, real captures replayed in CI | ✅ |
| Codex, Gemini CLI | ⚠️ untested | 🚧 stub |

Relay mode filters the reconstructed screen, so any TUI agent can work with the right `rules.json`. No code needed. Using grok, aider, opencode or cursor-agent? Open an issue with a capture (`SQUINT_CAPTURE=/tmp/x.raw sq`) and we'll add a rule set.

`--relay` needs a PTY, so it runs on macOS and Linux. Headless mode also runs on Windows.

## More

- [Customize](docs/customize.md): rename the step lines, add noise patterns, write a lens
- [How it works](docs/internals.md): PTY + terminal emulator, and why a line filter can't work
- [Contributing](CONTRIBUTING.md): captures, rule sets, new agents

## License

MIT
