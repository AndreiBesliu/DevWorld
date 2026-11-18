#!/usr/bin/env bash
# Tests of tools/scripts/annotate.sh: one GitHub Actions annotation from the
# text on standard input, escaped as the runner reads workflow commands.
#
#   tools/scripts/test/annotate.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../annotate.sh"
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

# annotate <stdin text> <argument>...: runs annotate.sh with that standard input.
annotate() {
  local text="$1"
  shift
  printf '%b' "${text}" | "${script}" "$@"
}

check 'lines become one error annotation' 0 --is '::error title=go test::--- FAIL: TestX%0A    x_test.go:3: got 1' -- \
  annotate '--- FAIL: TestX\n    x_test.go:3: got 1\n' 'go test'
check 'the last line counts without a line break' 0 --is '::error title=t::a%0Ab' -- annotate 'a\nb' t
check 'a notice' 0 --is '::notice title=e2e measurements::S1 p90 11 ms' -- annotate 'S1 p90 11 ms\n' 'e2e measurements' 40 notice
check 'a warning' 0 --is '::warning title=policy::still set' -- annotate 'still set\n' policy 5 warning
check 'percent signs are escaped and carriage returns dropped' 0 --is '::error title=t::50%25 done%0Anext' -- \
  annotate '50% done\r\nnext\r\n' t
check 'only the first <lines> lines are kept' 0 --is '::error title=t::one%0Atwo' -- annotate 'one\ntwo\nthree\n' t 2
check 'nothing on standard input prints nothing' 0 --is '' -- annotate '' t
check 'a title with a colon and a comma is escaped' 0 --is '::error title=PH0-22%3A scan%2C reports::match' -- \
  annotate 'match\n' 'PH0-22: scan, reports'
check 'a percent sign in the title is escaped' 0 --is '::error title=100%25 check::x' -- annotate 'x\n' '100% check'
check 'no title is a usage error' 2 --has usage -- "${script}"
check 'four arguments are a usage error' 2 --has usage -- annotate 'x\n' t 5 error extra
check 'a line count of 0 is a usage error' 2 --has usage -- annotate 'x\n' t 0
check 'a line count that is not a number is a usage error' 2 --has usage -- annotate 'x\n' t ten
check 'an unknown level is a usage error' 2 --has usage -- annotate 'x\n' t 5 debug

finish annotate.test.sh
