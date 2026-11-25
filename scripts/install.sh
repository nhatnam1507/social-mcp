#!/bin/sh
# Install the social-mcp binary that matches this machine's OS and CPU (Linux and macOS).
#
#   curl -fsSL https://github.com/nhatnam1507/social-mcp/releases/latest/download/install.sh | sh
#
# Environment:
#   SOCIAL_MCP_VERSION      release tag to install, for example v0.1.0 (default: latest release)
#   SOCIAL_MCP_INSTALL_DIR  folder to install into (default: $HOME/.local/bin)
#
# Everything runs inside main(), called on the last line, so a partially downloaded script never runs.
set -eu

REPO="nhatnam1507/social-mcp"

say() { printf '%s\n' "$*"; }
fail() {
	printf 'social-mcp install: %s\n' "$*" >&2
	exit 1
}

# download URL DEST
download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1" || fail "download failed: $1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1" || fail "download failed: $1"
	else
		fail "curl or wget is required"
	fi
}

# sha256 FILE: print the file's SHA-256 in lowercase hex.
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "sha256sum or shasum is required to verify the download"
	fi
}

main() {
	version="${SOCIAL_MCP_VERSION:-latest}"
	dir="${SOCIAL_MCP_INSTALL_DIR:-${HOME:?HOME is not set}/.local/bin}"

	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	MINGW* | MSYS* | CYGWIN*)
		fail "on Windows, run this in PowerShell instead: irm https://github.com/$REPO/releases/latest/download/install.ps1 | iex"
		;;
	*) fail "unsupported OS: $(uname -s) (supported: Linux, macOS)" ;;
	esac

	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported CPU: $(uname -m) (supported: amd64, arm64)" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple silicon; install the native build instead.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		arch=arm64
	fi

	if [ "$version" = latest ]; then
		base="https://github.com/$REPO/releases/latest/download"
	else
		base="https://github.com/$REPO/releases/download/$version"
	fi
	asset="social-mcp-$os-$arch"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t social-mcp)"
	trap 'rm -rf "$tmp"' EXIT
	trap 'rm -rf "$tmp"; exit 1' HUP INT TERM

	say "downloading $asset ($version)"
	download "$base/$asset" "$tmp/$asset"
	download "$base/SHA256SUMS" "$tmp/SHA256SUMS"

	expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/SHA256SUMS")"
	[ -n "$expected" ] || fail "SHA256SUMS has no entry for $asset"
	actual="$(sha256 "$tmp/$asset")"
	[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual); nothing was installed"

	mkdir -p "$dir"
	target="$dir/social-mcp"
	upgrade=false
	[ -e "$target" ] && upgrade=true
	# Copy next to the target, then rename over it: replacing a running binary this way is safe.
	cp "$tmp/$asset" "$dir/.social-mcp.tmp.$$"
	chmod 755 "$dir/.social-mcp.tmp.$$"
	mv -f "$dir/.social-mcp.tmp.$$" "$target"

	say "installed social-mcp $("$target" version 2>/dev/null || echo "$version") to $target"
	case ":$PATH:" in
	*":$dir:"*) ;;
	*)
		say ""
		say "$dir is not on your PATH. Add this line to your shell profile (~/.bashrc, ~/.zshrc, ...):"
		say "  export PATH=\"$dir:\$PATH\""
		;;
	esac
	if [ "$upgrade" = true ]; then
		say "A running daemon keeps the old version until you restart it: social-mcp daemon stop && social-mcp daemon start"
	fi
}

main "$@"
