#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
command -v go >/dev/null 2>&1 || { echo 'Install Go 1.24+ first.' >&2; exit 1; }
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/damocline" <<INSTALL
#!/bin/sh
cd '$ROOT' || exit 1
exec go run ./cmd/damocline "\$@"
INSTALL
chmod 755 "$HOME/.local/bin/damocline"
echo 'Source launcher installed: ~/.local/bin/damocline. Make sure ~/.local/bin is in PATH.'
