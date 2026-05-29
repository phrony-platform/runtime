#!/usr/bin/env sh
# Idempotently ensure phrony's install directory is on PATH in the user's shell rc.
set -eu

MARK_BEGIN="# >>> phrony runtime PATH >>>"
MARK_END="# <<< phrony runtime PATH <<<"

install_dir="${PHRONY_INSTALL_DIR:-${HOME}/.local/bin}"
path_line="export PATH=\"${install_dir}:\$(go env GOPATH 2>/dev/null)/bin:\$PATH\""

case "${SHELL:-}" in
	*/zsh) rc="${ZDOTDIR:-$HOME}/.zshrc" ;;
	*/bash) rc="${HOME}/.bashrc" ;;
	*)
		rc="${HOME}/.profile"
		[ -f "${HOME}/.zshrc" ] && rc="${HOME}/.zshrc"
		;;
esac

if [ -f "$rc" ] && grep -Fq "$MARK_BEGIN" "$rc"; then
	printf 'PATH already configured in %s\n' "$rc"
	exit 0
fi

mkdir -p "$(dirname "$rc")"
{
	printf '\n%s\n' "$MARK_BEGIN"
	printf '%s\n' "$path_line"
	printf '%s\n' "$MARK_END"
} >>"$rc"

printf 'Added %s to PATH in %s\n' "$install_dir" "$rc"
printf 'Open a new terminal or run: source %s\n' "$rc"
