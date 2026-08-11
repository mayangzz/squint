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

Requires Go 1.25+ and an agent CLI on your `PATH`.

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
| `merge_tool_runs` | consecutive calls to the *same tool* become one line with a count (default `true`) — see below |
| `noise` | lines to drop entirely |
| `head` | must have two capture groups: tool name, arguments |

**Runs of one tool become one line.** An agent that fires seven `sed` commands in a row produces seven identical-looking status lines that tell you nothing after the first. squint folds them:

```
🔍 running  sed -n 200,270p service/censor/createcensor.go ×7
```

The count and the command update in place while the run is live, so the line doubles as a heartbeat — if the number is climbing, it is still working. Set `merge_tool_runs: false` to list every call instead.

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
| **Claude Code** | ✅ tested against 2.1.227, with a real session capture in CI | ✅ |
| Codex | ⚠️ untested | 🚧 stub — command line is right, event mapping isn't |
| Gemini CLI | ⚠️ untested | 🚧 stub |
| everything else | — | — |

Relay mode is agent-agnostic: it filters the *reconstructed screen*, so it does not care how the agent paints. Any TUI can work with the right `rules.json` — no code needed. Headless mode needs a `Source` implementation (~80 lines, see `internal/source/claude.go`).

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
  keyboard  ───► │  relay: PTY + VT emulator    │ ───►  agent (unmodified)
  terminal  ◄─── │  filtered repaint            │ ◄───  raw TUI output
                 └──────────────────────────────┘

                 ┌──────────┐      ┌───────┐      ┌──────┐
  headless: ───► │  source  │ ───► │ Event │ ───► │ lens │ ───► terminal
                 └──────────┘      └───────┘      └──────┘
                  stream-json      normalized      what you see
```

Two pluggable axes meeting at one normalized `Event`: **sources** decide where events come from, **lenses** decide what you see. Adding a backend touches no lens; adding a lens touches no backend.

Relay mode runs a real terminal emulator, because there is no shortcut. Claude Code 2.1.227 paints its screen differentially: `\x1b[<n>A` to jump up, `\r\x1b[1B` to step down and patch individual cells, several screen rows packed into a single write, words positioned with `\x1b[<n>G` rather than separated by spaces. A "line" in the byte stream has no relationship to a line on screen — and deleting bytes from that stream corrupts the cursor arithmetic that follows.

So squint replays the output into an emulator, reads the reconstructed screen, and repaints a filtered copy. Two consequences fall out of that:

- **Rows that scroll off are appended once and never touched again**, so your terminal's native scrollback, mouse selection and find all keep working.
- **The live region can be filtered too.** The child's cursor moves inside the emulator, not on your screen, so squint is free to lay out the visible region however it likes.

```
internal/event    normalized Event
internal/source   agent backends — claude ✓, codex / gemini stubs
internal/lens     built-ins + external-process plugins
internal/relay    PTY + terminal emulator + rule-driven screen filter
```

---

## Status

MVP, and honest about it.

**Headless mode is solid.** Sources, lenses, `--save`/`--replay` all work and are covered by tests.

**Relay mode works against Claude Code 2.1.227.** A full session capture replays to 217 screen lines with 90 of them collapsed — that capture ships in `internal/relay/testdata/` and is a default test, so a UI change upstream breaks the build rather than silently doing nothing.

**What it costs.** Every frame repaints the visible region, so squint does more work than a dumb pipe. In practice that's one repaint per ~16ms of output; the emulator is the same code your terminal runs.

**Repaints are diffed.** A steady-state frame — a spinner ticking, you typing — changes one or two rows out of forty. squint rewrites only those rows; the rest are never touched, so there is nothing to flicker. A full erase-and-redraw happens only when the layout actually changes (new scrollback, different row count).

**Terminal modes pass straight through.** The emulator consumes the agent's output, but some of that output is not drawing — it configures *your terminal's input*: bracketed paste (`?2004`), mouse reporting, focus events, the kitty keyboard protocol, the window title. Those are forwarded verbatim. Swallowing them silently breaks features that have nothing to do with display: image paste stops working, mouse selection dies, the title freezes. squint changes what you see, never what the terminal can do.

If nothing gets collapsed at all, squint says so on exit rather than pretending to work:

```
squint: saw 412 lines and collapsed nothing — rule set "claude-code"
may not match this agent version. Run `squint --check`.
```

> **A warning for anyone editing rules.** Four things about this output are invisible on screen, and getting any of them wrong makes the rules match nothing — *silently*, because the display still looks fine:
>
> | | On screen | In the bytes |
> |---|---|---|
> | markers | `●` and `└` | `⏺` U+23FA and `⎿` U+23BF |
> | line ending | a new line | `\r\r\n` (the child writes `\r\n`, then ONLCR expands the `\n`) |
> | word spacing | spaces | no space bytes at all — `\x1b[<n>G` column jumps |
> | a line | a line | not a thing — the screen is patched cell by cell, several rows per write |
>
> That last one is why this project runs an emulator instead of a line filter. Capture real bytes before changing any of it; every bad bug here was one of these four, and the unit tests stayed green each time because the fixtures were transcribed from the screen too.

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
