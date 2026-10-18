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
# files count; a line counts unless it is blank or holds only a comment (a
# line that starts with //, or a /* ... */ block that starts its line).
# Prints "<folder>: <n> lines (budget <budget>)" per folder. Exits 1 when a
# folder has more lines than the budget, 2 on a usage error.
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

# An awk program that prints the number of code lines in the files it reads.
# shellcheck disable=SC2016 # awk, not the shell, reads the $ fields
code_lines='
  FNR == 1 { in_block = 0 }
  {
    line = $0
    sub(/^[ \t]+/, "", line)
    sub(/[ \t\r]+$/, "", line)
    if (in_block) {
      if (index(line, "*/")) in_block = 0
      next
    }
    if (line == "" || substr(line, 1, 2) == "//") next
    if (substr(line, 1, 2) == "/*") {
      if (!index(substr(line, 3), "*/")) in_block = 1
      next
    }
    n++
  }
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
