#!/usr/bin/env bash
# Counts the native code of each plugin folder and fails when one has more
# lines than the budget (APP-01, MV14: at most 400 lines per plugin and
# platform, so a budget of 400; the Rust of the Windows shell stays under 300,
# so a budget of 299).
#
#   tools/scripts/count-native-lines.sh <budget> <folder>...
#
# A folder is one plugin on one platform, for example
# apps/mobile/plugins/pip/android/src/main. Its Kotlin, Java, Swift and Rust
# files count; a line counts unless it is blank or holds only comments: code
# before a // or a /*, or after a */, makes it a code line. Block comments do
# not nest. Strings may hold comment markers: "..." strings, which in Rust may
# span lines, multi-line """ ... """ strings and Rust raw strings (r"...",
# r#"..."#), and a '"' character literal opens no string. Swift's #"..."#
# strings and quotes nested in a string template are not read: a comment
# marker in one of them can miscount its line.
# Prints "<folder>: <n> lines (budget <budget>)" per folder. The budget is a
# decimal number, also with leading zeros. Exits 1 when a folder has more
# lines than the budget, 2 on a usage error.
# Tested by apps/mobile/test/count-native-lines.test.ts.
set -euo pipefail

usage() {
  echo "usage: $0 <budget> <folder>..." >&2
  exit 2
}

[[ $# -ge 2 ]] || usage
budget="$1"
shift
[[ "${budget}" =~ ^[0-9]+$ ]] || usage
budget=$((10#${budget})) # (( )) would read 0400 as octal

# An awk program that prints the number of code lines in the files it reads.
# shellcheck disable=SC2016 # awk, not the shell, reads the $ fields
code_lines='
  # Whether the line holds anything but whitespace and comments. in_block
  # carries a /* ... */ across lines, raw_end the end of a raw string (""" or,
  # in Rust, a quote and the hashes of its r#") and in_str a Rust "..."
  # string. rust is set for a .rs file.
  function has_code(line,    i, n, c, pair, code) {
    n = length(line)
    for (i = 1; i <= n; ) {
      c = substr(line, i, 1)
      pair = substr(line, i, 2)
      if (raw_end != "") {
        if (substr(line, i, length(raw_end)) == raw_end) { i += length(raw_end); raw_end = "" } else i++
        if (c != " " && c != "\t" && c != "\r") code = 1
      } else if (in_str) {
        if (c == "\\") i += 2
        else { if (c == "\"") in_str = 0; i++ }
        if (c != " " && c != "\t" && c != "\r") code = 1
      } else if (in_block) {
        if (pair == "*/") { in_block = 0; i += 2 } else i++
      } else if (pair == "//") {
        break
      } else if (pair == "/*") {
        in_block = 1
        i += 2
      } else if (substr(line, i, 3) == "\"\"\"") {
        raw_end = "\"\"\""
        code = 1
        i += 3
      } else if (rust && match(substr(line, i), /^r#*"/)) {
        raw_end = "\"" substr(line, i + 1, RLENGTH - 2)
        code = 1
        i += RLENGTH
      } else if (substr(line, i, 3) == "\047\"\047") {
        code = 1
        i += 3
      } else if (substr(line, i, 4) == "\047\\\"\047") {
        code = 1
        i += 4
      } else if (c == "\"") {
        in_str = 1
        code = 1
        i++
      } else {
        if (c != " " && c != "\t" && c != "\r") code = 1
        i++
      }
    }
    # Only a Rust "..." string goes on to the next line.
    if (!rust) in_str = 0
    return code
  }
  FNR == 1 { in_block = 0; raw_end = ""; in_str = 0; rust = FILENAME ~ /\.rs$/ }
  has_code($0) { n++ }
  END { print n + 0 }'

status=0
for folder in "$@"; do
  if [[ ! -d "${folder}" ]]; then
    echo "${folder}: not a folder" >&2
    exit 2
  fi
  # find runs awk once per batch of files, so the batch counts are summed.
  lines="$(find "${folder}" -type f \( -name '*.kt' -o -name '*.java' -o -name '*.swift' -o -name '*.rs' \) \
    -exec awk "${code_lines}" {} + |
    awk '{ total += $1 } END { print total + 0 }')"
  if ((lines > budget)); then
    echo "${folder}: ${lines} lines, over the budget of ${budget}" >&2
    status=1
  else
    echo "${folder}: ${lines} lines (budget ${budget})"
  fi
done
exit "${status}"
