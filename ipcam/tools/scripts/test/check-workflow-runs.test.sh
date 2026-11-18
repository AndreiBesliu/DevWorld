#!/usr/bin/env bash
# Tests of tools/scripts/check-workflow-runs.sh with a fake `gh` that answers
# the two API calls the script makes (the workflow runs of a commit and the
# check runs of a check suite) from JSON files that each case writes.
#
#   tools/scripts/test/check-workflow-runs.test.sh
#
# Prints one line per case and exits 1 when a case fails.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${here}/../check-workflow-runs.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
# shellcheck source=tools/scripts/test/lib.sh
source "${here}/lib.sh"

repo=AndreiBesliu/DevWorld
sha=0123456789abcdef0123456789abcdef01234567

# The fake gh: `gh api [--paginate] <path> [--jq <filter>]`, answered from FAKE_GH.
mkdir -p "${tmp}/bin"
cat > "${tmp}/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == api ]] || { echo "fake gh: unexpected command $1" >&2; exit 2; }
shift
path="" filter=.
while [[ $# -gt 0 ]]; do
  case "$1" in
    --paginate) ;;
    --jq) filter="$2"; shift ;;
    *) path="$1" ;;
  esac
  shift
done
if [[ -n "${FAKE_GH_FAILS:-}" ]]; then
  echo 'gh: Resource not accessible by integration (HTTP 403)' >&2
  exit 1
fi
case "${path}" in
  "repos/${FAKE_REPO}/actions/runs?head_sha=${FAKE_SHA}&per_page=100") file="${FAKE_GH}/runs.json" ;;
  "repos/${FAKE_REPO}/check-suites/"*"/check-runs?per_page=100")
    suite="${path#*/check-suites/}"
    file="${FAKE_GH}/suite-${suite%%/*}.json"
    ;;
  *) file="" ;;
esac
if [[ -z "${file}" || ! -f "${file}" ]]; then
  echo "gh: Not Found (HTTP 404) for ${path}" >&2
  exit 1
fi
jq -r "${filter}" "${file}"
EOF
chmod +x "${tmp}/bin/gh"

# A case's answers: `runs <name> <id> <event> <status> <conclusion> <suite>...`
# (six words per run) writes runs.json; `jobs <suite> <name>:<status>:<conclusion>...`
# writes the check runs of a suite. `case_dir <name>` starts a case.
case_dir() {
  dir="${tmp}/$1"
  mkdir -p "${dir}"
}
runs() {
  local objects=()
  while [[ $# -gt 0 ]]; do
    objects+=("$(jq -n --arg name "$1" --argjson id "$2" --arg event "$3" --arg status "$4" --arg conclusion "$5" \
      --argjson suite "$6" '{name: $name, id: $id, event: $event, status: $status,
        conclusion: (if $conclusion == "null" then null else $conclusion end), check_suite_id: $suite}')")
    shift 6
  done
  printf '%s\n' "${objects[@]}" | jq -s '{total_count: length, workflow_runs: .}' > "${dir}/runs.json"
}
jobs() {
  local suite="$1" job name status conclusion
  shift
  for job in "$@"; do
    IFS=: read -r name status conclusion <<< "${job}"
    jq -n --arg name "${name}" --arg status "${status}" --arg conclusion "${conclusion}" \
      '{name: $name, status: $status, conclusion: (if $conclusion == "null" then null else $conclusion end)}'
  done | jq -s '{total_count: length, check_runs: .}' > "${dir}/suite-${suite}.json"
}
# gate <case dir> [<NAME=value>...] -- <workflow>...: runs the script on the case's answers.
gate() {
  local answers="$1" vars=()
  shift
  while [[ "$1" != -- ]]; do
    vars+=("$1")
    shift
  done
  shift
  env PATH="${tmp}/bin:${PATH}" FAKE_GH="${answers}" FAKE_REPO="${repo}" FAKE_SHA="${sha}" ${vars[@]+"${vars[@]}"} \
    "${script}" "${repo}" "${sha}" "$@"
}

case_dir green
runs ipcam-spikes 11 push completed success 101 \
  ipcam-spikes 12 pull_request completed success 102 \
  ipcam-apps 13 pull_request completed success 103 \
  ipcam-spike-release 14 workflow_dispatch in_progress null 104
jobs 101 go:completed:success media:completed:success e2e:completed:success
jobs 102 go:completed:success media:completed:success e2e:completed:success
jobs 103 js:completed:success 'Show failures:completed:skipped'
check 'every run and job of the named workflows succeeded' 0 \
  --has 'ipcam-spikes: 2 runs (pull_request, push), 6 jobs, all succeeded' \
  --has 'ipcam-apps: 1 run (pull_request), 2 jobs, all succeeded' --lacks 'ipcam-spike-release' -- \
  gate "${dir}" -- ipcam-spikes ipcam-apps

check 'a workflow without a run on the commit stops it' 1 --has "ipcam-mobile: no run on ${sha}" -- \
  gate "${dir}" -- ipcam-spikes ipcam-mobile

case_dir running
runs ipcam-spikes 21 push completed success 201 ipcam-spikes 22 pull_request in_progress null 202
jobs 201 go:completed:success
jobs 202 go:completed:success e2e:in_progress:null
check 'a run that has not finished stops it' 1 --has 'ipcam-spikes run 22 (pull_request): in_progress, no conclusion' -- \
  gate "${dir}" -- ipcam-spikes

case_dir failed
runs ipcam-windows 31 pull_request completed failure 301
jobs 301 checks:completed:success windows:completed:failure
check 'a failed run stops it' 1 --has 'ipcam-windows run 31 (pull_request): completed, failure' -- \
  gate "${dir}" -- ipcam-windows

case_dir job
runs ipcam-mobile 41 pull_request completed success 401
jobs 401 android:completed:success emulator:completed:cancelled
check 'a job that did not succeed stops it, whatever its run says' 1 \
  --has 'ipcam-mobile run 41 (pull_request), job emulator: completed, cancelled' -- \
  gate "${dir}" -- ipcam-mobile

case_dir nojobs
runs ipcam-apps 51 push completed success 501
jobs 501
check 'a run without jobs stops it' 1 --has 'ipcam-apps run 51 (push): no check runs' -- \
  gate "${dir}" -- ipcam-apps

check 'an API error stops it with the error' 1 --has 'HTTP 403' -- gate "${tmp}/green" FAKE_GH_FAILS=1 -- ipcam-spikes

check 'no workflow is a usage error' 2 --has usage -- gate "${tmp}/green" --
check 'a short commit is a usage error' 2 --has usage -- \
  env PATH="${tmp}/bin:${PATH}" "${script}" "${repo}" 0123abc ipcam-spikes

finish check-workflow-runs.test.sh
