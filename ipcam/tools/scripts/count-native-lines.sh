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
# not nest, and a "..." string or a multi-line """ ... """ string may hold
# comment markers. A ' character literal that holds a quote ('"') is not
# read, so a line with one may be counted too high (never too low).
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
  # carries a /* ... */ across lines, in_raw a """ ... """ string.
  function has_code(line,    i, n, c, pair, triple, code) {
    n = length(line)
    for (i = 1; i <= n; ) {
      c = substr(line, i, 1)
      pair = substr(line, i, 2)
      triple = substr(line, i, 3)
      if (in_raw) {
        if (triple == "\"\"\"") { in_raw = 0; i += 3 } else i++
        if (c != " " && c != "\t" && c != "\r") code = 1
      } else if (in_block) {
        if (pair == "*/") { in_block = 0; i += 2 } else i++
      } else if (pair == "//") {
        break
      } else if (pair == "/*") {
        in_block = 1
        i += 2
      } else if (triple == "\"\"\"") {
        in_raw = 1
        code = 1
        i += 3
      } else if (c == "\"") {
        code = 1
        for (i++; i <= n && substr(line, i, 1) != "\""; i++) if (substr(line, i, 1) == "\\") i++
        i++
      } else {
        if (c != " " && c != "\t" && c != "\r") code = 1
        i++
      }
    }
    return code
  }
  FNR == 1 { in_block = 0; in_raw = 0 }
  has_code($0) { n++ }
  END { print n + 0 }
'

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
