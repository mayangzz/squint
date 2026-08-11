# squint

**Squint at your coding agent.** Blur the machinery, keep what you asked for.

```
⏺ Bash(sed -n 280,300p service/risk/chat.go; echo === ; sed -n 55,75p report.go)
  ⎿ Error: Exit code 1                                          ┐
            }                                                   │
        for _, logMap := range result {                         │  you never
    … +13 lines (ctrl+o to expand)                              │  read any
                                                                │  of this
  Searched for 1 pattern (ctrl+o to expand)                     │
  Shell cwd was reset to /Users/you/repo                        ┘
```

becomes

```
🔍 running  sed -n 280,300p service/risk/chat.go; echo === ; …
```

---

## Why

A coding agent narrates every move: the full shell command, a preview of its output, `… +78 lines (ctrl+o to expand)`, a stray `Shell cwd was reset to …`. You scroll past all of it looking for the two sentences that actually answer your question.

That noise is not free. It pushes the answer off-screen, it makes long sessions unreadable, and — the part people underestimate — it costs attention. You end up **reading a transcript of a shell session you didn't want to read**, when all you needed was "it's looking at the risk logic".

squint gives you the second thing:

```
🔍 running  go test ./...
📖 reading  internal/relay/filter.go
🔎 searching  CensorStrategyUserCheck
✏️  editing  service/censor/payload.go
```

Glanceable. No parsing required. You look up, see where it is, look away.

**It changes nothing about how the agent runs.** Same permissions, same slash commands, same Esc-to-interrupt, same everything. Only the display changes. If you don't like it, stop using the wrapper — there is nothing to undo.

If you live in a terminal and run an agent for hours a day, this is the difference between a wall of text and a status line.

---

## Install

```bash
go install github.com/mayangzz/squint@latest
```

Requires Go 1.24+ and an agent CLI on your `PATH`.

---

## Use

### Bind it to your agent

The intended setup: keep typing what you already type.

```bash
# ~/.zshrc or ~/.bashrc — wrap the agent, keep the name you're used to
sq() { squint --relay claude "$@"; }
```

Now `sq` is your normal session with the noise turned down. Keeping `claude` untouched next to it means you can A/B any time, and fall back instantly if a rule misfires.

Prefer to replace it outright? An alias works, as long as it doesn't recurse:

```bash
alias claude='squint --relay command claude'
```

Extra arguments pass straight through: `sq --resume`, `sq --model opus`.

### One-shot, no interaction

```bash
squint "explain what this repo does"
squint --lens quiet "fix the failing test"
squint --save run.jsonl "refactor the parser"
squint --replay run.jsonl --lens errors      # re-read a past run, costs nothing
```

| Lens | Shows |
|---|---|
| `minimal` *(default)* | one line per step, the answer, then `N steps · 12.4s · $0.03` |
| `trace` | same, plus the raw command under each step |
| `quiet` | the answer only |
| `errors` | failed steps only |

---

## Make it yours

### Rename the steps

Don't like `🔍 running`? Every line is a template. Drop a `~/.squint/rules.json`:

```json
{
  "name": "mine",
  "head": "^\\s*[⏺●○*]\\s+(\\w+)\\((.*)$",
  "branch": "^\\s*[⎿└├│]",
  "head_format": "● {tool}({arg})",
  "tools": {
    "Bash":  "🔍 正在查找  {arg}",
    "Read":  "📖 读        {arg}",
    "Edit":  "✏️  改        {arg}",
    "Grep":  "🔎 搜        {arg}",
    "Task":  "🤖 派活      {arg}"
  },
  "dedupe_repeats": true,
  "noise": ["^\\s*…\\s*\\+\\d+ lines", "^\\s*Shell cwd was reset to "]
}
```

| Key | |
|---|---|
| `tools` | per-tool line. Placeholders `{tool}` `{arg}`. Names are case-insensitive |
| `head_format` | fallback for tools you didn't name |
| `dedupe_repeats` | consecutive identical lines print once (default `true`) — an agent re-reading the same file five times tells you nothing the second time |
| `noise` | lines to drop entirely |
| `head` | must have two capture groups: tool name, arguments |

Start from the shipped defaults:

```bash
mkdir -p ~/.squint && curl -o ~/.squint/rules.json \
  https://raw.githubusercontent.com/mayangzz/squint/main/internal/relay/rules.json
squint --check    # verifies your rules still collapse a real sample
```

### Write a lens

For headless mode, a lens is any executable in `~/.squint/lenses/`. squint feeds it one JSON event per line on stdin; whatever it prints is displayed. Print nothing to hide the event.

```python
#!/usr/bin/env python3
import json, sys

VERB = {"Read": "📖", "Write": "✏️", "Bash": "🔍", "Grep": "🔎"}

for line in sys.stdin:
    e = json.loads(line)
    if e["kind"] == "step":
        print(f"  {VERB.get(e.get('tool',''),'•')} {e['label']}", flush=True)
    elif e["kind"] == "done":
        print(f"  —— {e['ms']/1000:.1f}s ${e['cost']:.4f}", flush=True)
    else:
        print("", flush=True)
```

```bash
chmod +x ~/.squint/lenses/verbs && squint --lens verbs "..."
```

| Event field | |
|---|---|
| `kind` | `start` / `step` / `text` / `error` / `done` |
| `label` | human sentence — the agent's own `description` when it has one |
| `detail` | command, path, or pattern |
| `tool` | `Bash` / `Read` / `Grep` / … |
| `cost`, `ms` | on `done` |

Any language. One JSON object in, one line out.

---

## Supported agents

| Agent | Relay (interactive) | Headless |
|---|---|---|
| **Claude Code** | ✅ tested against 2.1.227 | ✅ |
| Codex | ⚠️ untested | 🚧 stub — command line is right, event mapping isn't |
| Gemini CLI | ⚠️ untested | 🚧 stub |
| everything else | — | — |

Relay mode is mostly agent-agnostic: it filters on-screen text, so any agent whose TUI appends lines can work with the right `rules.json` — no code needed. Headless mode needs a `Source` implementation (~80 lines, see `internal/source/claude.go`).

**Using something else — grok, aider, opencode, cursor-agent?** Open an issue with a snippet of its output (or better, a PTY capture) and we'll add a rule set. Adding your agent is usually a JSON file, not a patch.

---

## Configure

`~/.squint/config.json`:

```json
{ "lens": "minimal", "source": "claude", "model": "" }
```

Flags override the file.

---

## How it works

```
                 ┌──────────────────────────────┐
  keyboard  ───► │  relay: PTY + line filter    │ ───►  agent (unmodified)
  terminal  ◄─── │  collapses tool blocks       │ ◄───  raw TUI output
                 └──────────────────────────────┘

                 ┌──────────┐      ┌───────┐      ┌──────┐
  headless: ───► │  source  │ ───► │ Event │ ───► │ lens │ ───► terminal
                 └──────────┘      └───────┘      └──────┘
                  stream-json      normalized      what you see
```

Two pluggable axes meeting at one normalized `Event`: **sources** decide where events come from, **lenses** decide what you see. Adding a backend touches no lens; adding a lens touches no backend.

Relay mode is feasible because Claude Code uses the classic main-screen renderer — no alternate screen, no `[2J`. Committed output is appended line by line, so a line filter is enough; no terminal emulator required. It does use cursor addressing (`\x1b[<n>G` to place words, `\x1b[<n>A` to repaint the live region), which is why squint reconstructs column positions before matching and leaves the live region alone.

```
internal/event    normalized Event
internal/source   agent backends — claude ✓, codex / gemini stubs
internal/lens     built-ins + external-process plugins
internal/relay    PTY wrapper + rule-driven line filter
```

---

## Status

MVP, and honest about it.

**Headless mode is solid.** Sources, lenses, `--save`/`--replay` all work and are covered by tests.

**Relay mode has a known gap.** Claude Code redraws its *in-progress* region — input box, spinner, the tool block still running — by moving the cursor up (`\x1b[nA`) and repainting. squint cannot collapse those lines: collapsing changes how many lines there are, and the next repaint would land on the wrong ones. So you get collapsing on committed scrollback and raw output on the live region. Short tasks may see little effect.

If nothing gets collapsed at all, squint says so on exit rather than pretending to work:

```
squint: saw 412 lines and collapsed nothing — rule set "claude-code"
may not match this agent version. Run `squint --check`.
```

> **A warning for anyone editing rules.** Three things about this output are invisible on screen, and getting any of them wrong makes the rules match nothing — *silently*, because the display still looks fine:
>
> | | On screen | In the bytes |
> |---|---|---|
> | markers | `●` and `└` | `⏺` U+23FA and `⎿` U+23BF |
> | line ending | a new line | `\r\r\n` (the child writes `\r\n`, then ONLCR expands the `\n`) |
> | word spacing | spaces | no space bytes at all — `\x1b[<n>G` column jumps |
>
> Capture real bytes before changing these. Every bad bug in this project was one of these three, and the unit tests stayed green each time because the fixtures were transcribed from the screen too.

Builds clean for darwin, linux and windows. `--relay` is Unix-only (no PTY on Windows); headless mode works everywhere.

## Contributing

Issues and PRs welcome — especially:

- **rule sets for other agents** (a `rules.json` + a sample of the output you captured)
- **`Source` implementations** for codex / gemini / anything with a streaming JSON mode
- **lenses you found useful** — they're standalone scripts, easy to share

If a rule stops matching after an agent update, that's a bug worth reporting even without a fix; `squint --check` output is the useful thing to paste.

For anything that feels wrong in relay mode — laggy, frozen, something swallowed — capture the raw stream instead of describing it:

```bash
SQUINT_CAPTURE=/tmp/squint.raw claude    # or however you invoke it
squint --check /tmp/squint.raw           # replay it through the rules
```

`--check <capture>` tells you which layer is broken:

| Output | Means |
|---|---|
| `no line ever formed` | line-ending detection is wrong for this agent |
| `most output never reached the filter` | a squint bug, not your rules |
| `lines formed but no rule matched` | the rule set doesn't fit this agent |

Attach the capture to the issue. Every serious bug in this project was invisible from the rendered output and obvious from the bytes — line endings turned out to be `\r\r\n`, and words are positioned with `\x1b[<n>G` rather than separated by spaces. A screenshot shows neither.

## License

MIT
