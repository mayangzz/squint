# Customizing squint

## Rename the steps

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
🔍 running  sed -n 200,270p service/bill/createinvoice.go ×7
```

The count and the command update in place while the run is live, so the line doubles as a heartbeat — if the number is climbing, it is still working. Set `merge_tool_runs: false` to list every call instead.

Start from the shipped defaults:

```bash
mkdir -p ~/.squint && curl -o ~/.squint/rules.json \
  https://raw.githubusercontent.com/mayangzz/squint/main/internal/relay/rules.json
squint --check    # verifies your rules still collapse a real sample
```

## Write a lens

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

## Pick a rule file per shell

`SQUINT_RULES` overrides `~/.squint/rules.json` for one process: a path to another rule set, or `builtin` to ignore your file and use the shipped rules.

```bash
SQUINT_RULES=builtin squint --relay claude
```

## Config file

`~/.squint/config.json`:

```json
{ "lens": "minimal", "source": "claude", "model": "" }
```

Flags override the file.
