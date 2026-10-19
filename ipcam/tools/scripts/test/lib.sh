# Helpers for the shell tests of tools/scripts, sourced by each *.test.sh.
# shellcheck shell=bash

failures=0

# check <case> <exit code> [--is <text>] [--has <text>]... [--lacks <text>]... -- <command>...
# Runs the command and checks its exit code and its output (stdout and stderr
# together): all of it (--is), or what it holds and lacks. Prints one line for
# the case, and on a failure what was wrong and the output.
check() {
  local name="$1" want="$2" is="" exact=0 has=() lacks=() out code=0 text problems=()
  shift 2
  while [[ "$1" != -- ]]; do
    case "$1" in
      --is)
        is="$2"
        exact=1
        ;;
      --has) has+=("$2") ;;
      --lacks) lacks+=("$2") ;;
      *)
        echo "check: unknown option $1" >&2
        return 2
        ;;
    esac
    shift 2
  done
  shift
  out="$("$@" 2>&1)" || code=$?
  [[ ${code} -eq ${want} ]] || problems+=("exit ${code}, want ${want}")
  if [[ ${exact} -eq 1 && "${out}" != "${is}" ]]; then
    problems+=("the output is not: ${is}")
  fi
  for text in ${has[@]+"${has[@]}"}; do
    [[ "${out}" == *"${text}"* ]] || problems+=("the output lacks: ${text}")
  done
  for text in ${lacks[@]+"${lacks[@]}"}; do
    [[ "${out}" != *"${text}"* ]] || problems+=("the output holds: ${text}")
  done
  if [[ ${#problems[@]} -gt 0 ]]; then
    printf 'FAIL %s\n' "${name}"
    printf '     %s\n' "${problems[@]}"
    printf '     output:\n'
    printf '%s\n' "${out}" | sed 's/^/     | /'
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "${name}"
  fi
}

# finish <test name>: prints the outcome and exits 1 when a case failed.
finish() {
  if [[ ${failures} -gt 0 ]]; then
    echo "$1: ${failures} failed"
    exit 1
  fi
  echo "$1: all passed"
}
