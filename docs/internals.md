# How squint works

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

## Status and limits

MVP, and honest about it.

**Headless mode is solid.** Sources, lenses, `--save`/`--replay` all work and are covered by tests.

**Relay mode is tested against Claude Code 2.1.227 and 2.1.284.** Real session captures of both ship in `internal/relay/testdata/` and replay in the default test run, so a UI change upstream breaks the build rather than silently doing nothing.

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
