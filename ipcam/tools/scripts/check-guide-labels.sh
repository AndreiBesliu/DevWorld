#!/usr/bin/env bash
# Checks an owner guide against the programs it describes. Every text that the
# guide quotes between « and » names its program right after the closing »,
# in an HTML comment that the rendered guide does not show:
#
#   Apăsați «Verifică conexiunea»<!--app-->.
#
# and must occur, as written, in the files of that program: in at least one
# file of each file set given for it. Name a program more than once to require
# its texts in each set, for example in the sources and in the build:
#
#   tools/scripts/check-guide-labels.sh <guide.md> --program <name> <file>... [--program <name> <file>...]...
#
# In a quote, "…" stands for any text, so «gata în … s» matches the app's
# "gata în {seconds} s". A quote shorter than 4 characters, such as «Da», must
# stand between string delimiters (' " ` or the > < of a tag) in the file, so
# that it is not found inside a longer word. The guides quote the texts of
# Windows, Android and the browsers with „ ” instead: those differ by version
# and language, and are not checked. An empty «» quotes nothing.
#
# Prints each quote that its program's files lack and exits 1 when there is
# one, when a quote names no program or a program without files, when a quote
# is not closed on its line or holds another quote, or when the guide quotes
# nothing (a sign that the quotes were not read). Exits 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 <guide.md> --program <name> <file>... [--program <name> <file>...]..." >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
guide="$1"
shift
[[ -f "${guide}" ]] || {
  echo "${guide}: no such file" >&2
  exit 2
}
[[ $# -ge 1 && "$1" == --program ]] || usage

# The file sets: set_program[i] names the program of set i, set_files[i] holds
# its files, one per line.
set_program=()
set_files=()
while [[ $# -gt 0 ]]; do
  [[ "$1" == --program && $# -ge 2 && "$2" != --program ]] || usage
  set_program+=("$2")
  shift 2
  files=""
  while [[ $# -gt 0 && "$1" != --program ]]; do
    [[ -f "$1" ]] || {
      echo "$1: no such file" >&2
      exit 2
    }
    files+="$1"$'\n'
    shift
  done
  [[ -n "${files}" ]] || usage
  set_files+=("${files}")
done

# The guide is read as UTF-8 text; the programs as bytes (they may be
# binaries), so each quote is matched as its UTF-8 bytes.
export LC_ALL=C.UTF-8
problems=0
if grep -n '«[^»]*$' "${guide}"; then
  echo "${guide}: the quotes above are not closed on their line"
  problems=$((problems + 1))
fi
if grep -n '«[^»]*«' "${guide}"; then
  echo "${guide}: the lines above hold a quote inside a quote; quote the inner text alone"
  problems=$((problems + 1))
fi

# Each quote with its program and line, separated by the unit separator (US,
# 0x1f), which no guide holds: "<program>US<line>US<quote>". A quote that names
# no program has an empty program.
sep=$'\x1f'
pairs="$(grep -n -o '«[^«»]\+»\(<!--[a-z0-9-]*-->\)\?' "${guide}" |
  sed -e "s/^\([0-9]*\):«\(.*\)»<!--\(.*\)-->\$/\3${sep}\1${sep}\2/" -e "s/^\([0-9]*\):«\(.*\)»\$/${sep}\1${sep}\2/" || true)"
if [[ -z "${pairs}" ]]; then
  echo "${guide} quotes no text between « and »"
  exit 1
fi

has_program() {
  local i
  for i in "${!set_program[@]}"; do
    [[ "${set_program[i]}" == "$1" ]] && return 0
  done
  return 1
}

declare -A checked=()
missing=0
while IFS="${sep}" read -r program line quote; do
  if [[ -z "${program}" ]]; then
    echo "${guide}:${line}: «${quote}» names no program; add <!--<program>--> right after it"
    problems=$((problems + 1))
    continue
  fi
  if ! has_program "${program}"; then
    echo "${guide}:${line}: «${quote}» names the program ${program}, which has no files here"
    problems=$((problems + 1))
    continue
  fi
  key="${program}${sep}${quote}"
  [[ -z "${checked["${key}"]+x}" ]] || continue
  checked["${key}"]=1

  # The quote as an extended regular expression: its special characters
  # escaped, and each "…" turned into ".*"; a short one between delimiters.
  pattern="$(printf '%s' "${quote}" | sed -e 's/[][\.^$*+?(){}|]/\\&/g' -e 's/…/.*/g')"
  if [[ ${#quote} -lt 4 ]]; then
    pattern="(^|[\"'\`>])${pattern}([\"'\`<]|$)"
  fi
  found=1
  for i in "${!set_program[@]}"; do
    [[ "${set_program[i]}" == "${program}" ]] || continue
    mapfile -t files < <(printf '%s' "${set_files[i]}")
    if ! LC_ALL=C grep -aqE -- "${pattern}" "${files[@]}"; then
      names="$(for file in "${files[@]}"; do basename "${file}"; done | paste -sd, - | sed 's/,/, /g')"
      echo "not found in ${program} (${names}): «${quote}»"
      found=0
    fi
  done
  [[ ${found} -eq 1 ]] || missing=$((missing + 1))
done <<< "${pairs}"

echo "${guide}: ${#checked[@]} quoted texts checked, ${missing} not found"
[[ ${missing} -eq 0 && ${problems} -eq 0 ]]
