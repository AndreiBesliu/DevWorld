#!/usr/bin/env bash
# Checks an owner guide against the programs it describes: every text that the
# guide quotes between « and » must occur, as written, in at least one of the
# given files (a spikehub binary, which embeds the spike app, the APK, a
# shell's sources). In a quote, "…" stands for any text, so «gata în … s»
# matches the app's "gata în {seconds} s". The guides quote the texts of
# Windows, Android and the browsers with „ ” instead: those differ by version
# and language, and are not checked. An empty «» quotes nothing.
#
#   tools/scripts/check-guide-labels.sh <guide.md> <file>...
#
# Prints each quote that no file contains and exits 1 when there is one, when
# a quote is not closed on its line, or when the guide quotes nothing (a sign
# that the quotes were not read). Exits 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 <guide.md> <file>..." >&2
  exit 2
}

[[ $# -ge 2 ]] || usage
guide="$1"
shift
for file in "${guide}" "$@"; do
  [[ -f "${file}" ]] || {
    echo "${file}: no such file" >&2
    exit 2
  }
done

# The guide is read as UTF-8 text; the programs as bytes (they may be
# binaries), so each quote is matched as its UTF-8 bytes.
if LC_ALL=C.UTF-8 grep -n '«[^»]*$' "${guide}"; then
  echo "${guide}: the quotes above are not closed on their line" >&2
  exit 1
fi
mapfile -t quotes < <(LC_ALL=C.UTF-8 grep -o '«[^»]\+»' "${guide}" | sed -e 's/^«//' -e 's/»$//' | LC_ALL=C sort -u)
if [[ ${#quotes[@]} -eq 0 ]]; then
  echo "${guide} quotes no text between « and »" >&2
  exit 1
fi

missing=0
for quote in "${quotes[@]}"; do
  # The quote as an extended regular expression: its special characters
  # escaped, and each "…" turned into ".*".
  pattern="$(printf '%s' "${quote}" | LC_ALL=C sed -e 's/[][\.^$*+?(){}|]/\\&/g' -e 's/…/.*/g')"
  if ! LC_ALL=C grep -aqE -- "${pattern}" "$@"; then
    echo "not found in the programs: «${quote}»"
    missing=$((missing + 1))
  fi
done
echo "${guide}: ${#quotes[@]} quoted texts checked, ${missing} not found"
[[ ${missing} -eq 0 ]]
