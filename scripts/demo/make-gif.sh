#!/bin/sh
# Records both sessions and stitches them side by side into docs/demo.gif.
# Needs vhs, ffmpeg and rsvg-convert. Run from the repo root after scripts/demo/setup.sh.
# Each run costs two real agent sessions.
set -e
D=scripts/demo
vhs $D/stock.tape && vhs $D/squint.tape
rsvg-convert $D/label-stock.svg -o /tmp/label-stock.png
rsvg-convert $D/label-squint.svg -o /tmp/label-squint.png
ffmpeg -loglevel error -y -i stock.mp4 -i squint.mp4 -i /tmp/label-stock.png -i /tmp/label-squint.png -filter_complex "\
[0:v]trim=0:42,setpts=(PTS-STARTPTS)/2,scale=700:-2,tpad=stop_mode=clone:stop_duration=3[a0];[2:v][a0]vstack[a];\
[1:v]trim=0:42,setpts=(PTS-STARTPTS)/2,scale=700:-2,tpad=stop_mode=clone:stop_duration=3[b0];[3:v][b0]vstack[b];\
[a][b]hstack=shortest=1,fps=12,split[s0][s1];[s0]palettegen=max_colors=128[p];[s1][p]paletteuse=dither=bayer:bayer_scale=4" docs/demo.gif
rm -f stock.mp4 squint.mp4
ls -la docs/demo.gif
