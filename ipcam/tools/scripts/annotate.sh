#!/usr/bin/env bash
# Prints the text on stdin as one GitHub Actions annotation, an error unless
# <level> says otherwise. The ipcam workflows put a failure's cause there, and
# what a run measured, because check-run annotations can be read where a job
# log cannot (gh api repos/<owner>/<repo>/check-runs/<job id>/annotations).
#
#   <command> | tools/scripts/annotate.sh <title> [<lines>] [error|warning|notice]
#
# Keeps the first <lines> lines (default 150), drops carriage returns and
# escapes % and the line breaks, so the annotation shows the text as it was.
# The title is escaped as the runner reads a property of a workflow command
# (%, CR, LF, ":" and ","), so any text can be a title. Prints nothing when
# stdin is empty. Exits 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 <title> [<lines>] [error|warning|notice]" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 3 ]] || usage
title="$1"
lines="${2:-150}"
level="${3:-error}"
[[ "${lines}" =~ ^[1-9][0-9]*$ ]] || usage
[[ "${level}" =~ ^(error|warning|notice)$ ]] || usage

# awk reads all of stdin, so the command that writes it never meets a closed pipe.
awk -v title="${title}" -v lines="${lines}" -v level="${level}" '
  BEGIN {
    ORS = ""
    gsub(/%/, "%25", title)
    gsub(/\r/, "%0D", title)
    gsub(/\n/, "%0A", title)
    gsub(/:/, "%3A", title)
    gsub(/,/, "%2C", title)
  }
  NR <= lines {
    gsub(/\r/, "")
    gsub(/%/, "%25")
    print (NR > 1 ? "%0A" : "::" level " title=" title "::") $0
  }
  END { if (NR) print "\n" }'
