#!/usr/bin/env bash
# Checks through the GitHub API (gh api) that the named workflows ran on a
# commit and that every run of them, and every check run (job) of those runs,
# succeeded. The spike release publishes only a commit that passes it.
#
#   tools/scripts/check-workflow-runs.sh <owner/repo> <commit sha> <workflow name>...
#
# A job that was skipped counts as passed (it did not run, so it did not fail).
# Prints one line per workflow, and each problem: a workflow without a run on
# the commit, a run that has not finished or did not succeed, a run without
# check runs, a job that did not succeed. Exits 1 when there is a problem or
# the API cannot be read, 2 on a usage error. The token needs read access to
# actions and checks (GH_TOKEN, as gh reads it).
set -euo pipefail

usage() {
  echo "usage: $0 <owner/repo> <commit sha> <workflow name>..." >&2
  exit 2
}

[[ $# -ge 3 ]] || usage
repo="$1"
sha="$2"
shift 2
[[ "${repo}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ && "${sha}" =~ ^[0-9a-f]{40}$ ]] || usage

# Every run on the commit: "<workflow>\t<run id>\t<event>\t<status>\t<conclusion>\t<check suite id>".
runs="$(gh api --paginate "repos/${repo}/actions/runs?head_sha=${sha}&per_page=100" \
  --jq '.workflow_runs[] | [.name, .id, .event, .status, (.conclusion // "none"), .check_suite_id] | @tsv')"

problems=()
for workflow in "$@"; do
  mine="$(awk -F'\t' -v name="${workflow}" '$1 == name' <<< "${runs}")"
  if [[ -z "${mine}" ]]; then
    problems+=("${workflow}: no run on ${sha}")
    continue
  fi
  count=0 jobs=0 events=() bad=0
  while IFS=$'\t' read -r _ id event status conclusion suite; do
    count=$((count + 1))
    events+=("${event}")
    if [[ "${status}" != completed || "${conclusion}" != success ]]; then
      if [[ "${conclusion}" == none ]]; then
        problems+=("${workflow} run ${id} (${event}): ${status}, no conclusion")
      else
        problems+=("${workflow} run ${id} (${event}): ${status}, ${conclusion}")
      fi
      bad=1
      continue
    fi
    checks="$(gh api --paginate "repos/${repo}/check-suites/${suite}/check-runs?per_page=100" \
      --jq '.check_runs[] | [.name, .status, (.conclusion // "none")] | @tsv')"
    if [[ -z "${checks}" ]]; then
      problems+=("${workflow} run ${id} (${event}): no check runs")
      bad=1
      continue
    fi
    while IFS=$'\t' read -r job job_status job_conclusion; do
      jobs=$((jobs + 1))
      if [[ "${job_status}" != completed || ! "${job_conclusion}" =~ ^(success|skipped)$ ]]; then
        problems+=("${workflow} run ${id} (${event}), job ${job}: ${job_status}, ${job_conclusion/#none/no conclusion}")
        bad=1
      fi
    done <<< "${checks}"
  done <<< "${mine}"
  if [[ ${bad} -eq 0 ]]; then
    run_noun=runs job_noun=jobs
    [[ ${count} -ne 1 ]] || run_noun=run
    [[ ${jobs} -ne 1 ]] || job_noun=job
    sorted="$(printf '%s\n' "${events[@]}" | LC_ALL=C sort -u | paste -sd, - | sed 's/,/, /g')"
    echo "${workflow}: ${count} ${run_noun} (${sorted}), ${jobs} ${job_noun}, all succeeded"
  fi
done

if [[ ${#problems[@]} -gt 0 ]]; then
  printf '%s\n' "${problems[@]}"
  echo "the workflows above have not all succeeded on ${sha}"
  exit 1
fi
