# Sourced (hidden) at the start of each tape.
cd /tmp/shop
unset CLAUDECODE $(env | grep -oE '^CLAUDE_CODE_[A-Z_]+')
export PATH=/tmp/shop-bin:$PATH PS1='$ ' SQUINT_RULES=builtin
sq() { squint --relay claude "$@" 2>/dev/null; }
clear
