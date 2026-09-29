# Contributing

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

## Re-recording the demo

The GIF in the README is scripted, so it can be redone after a Claude Code UI change:

```bash
scripts/demo/setup.sh       # throwaway repo in /tmp/shop + a claude shim that skips your user config
scripts/demo/make-gif.sh    # records stock vs squint with vhs and writes docs/demo.gif
```

Each run is two real agent sessions. Record only in the throwaway repo, never in a private one.
