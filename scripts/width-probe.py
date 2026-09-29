#!/usr/bin/env python3
"""量一量你的终端到底把每个字符画成几格，跟 squint 的假设对不上就是错位的根源。

必须在真终端里直接跑（不能经管道）：python3 scripts/width-probe.py
"""
import sys, termios, tty, unicodedata

# CC 界面里出现、且各终端宽度判定容易分歧的字符
SUSPECTS = "─│╭╮╰╯━┃▐▛█▜▝▘⏵⏸⎇●·←→✳⏺⎿❯…✓✗"


def cursor_col() -> int:
    sys.stdout.write("\x1b[6n")
    sys.stdout.flush()
    buf = ""
    while not buf.endswith("R"):
        buf += sys.stdin.read(1)
    return int(buf[buf.index(";") + 1:-1])


def main():
    if not sys.stdin.isatty():
        sys.exit("要在真终端里直接跑，别接管道")
    old = termios.tcgetattr(sys.stdin)
    tty.setraw(sys.stdin)
    try:
        rows = []
        for ch in SUSPECTS:
            sys.stdout.write("\r\x1b[2K")
            before = cursor_col()
            sys.stdout.write(ch)
            rows.append((ch, cursor_col() - before))
        sys.stdout.write("\r\x1b[2K")
    finally:
        termios.tcsetattr(sys.stdin, termios.TCSADRAIN, old)

    ea = {ch: unicodedata.east_asian_width(ch) for ch, _ in rows}
    print("字符  U+      终端画几格  Unicode东亚宽度  squint假设")
    bad = 0
    for ch, w in rows:
        assumed = 2 if ea[ch] in ("W", "F") else 1   # squint/Go 的口径：歧义算 1 格
        flag = "  ← 不一致" if w != assumed else ""
        if w != assumed:
            bad += 1
        print(f" {ch}    {ord(ch):04X}       {w}            {ea[ch]}              {assumed}{flag}")
    print(f"\n不一致 {bad} 个。只要有一个出现在满宽行里，那行就会折行，squint 的行数模型随即错位。")


if __name__ == "__main__":
    main()
