#!/usr/bin/env bash
# Prints the text on stdin as one GitHub Actions error annotation. The ipcam
# workflows put a failure's cause there, because check-run annotations can be
# read where a job log cannot (gh api
# repos/<owner>/<repo>/check-runs/<job id>/annotations).
#
#   <command> | tools/scripts/annotate.sh <title> [<lines>]
#
# Keeps the first <lines> lines (default 150), drops carriage returns and
# escapes % and the line breaks, so the annotation shows the text as it was.
# The title is plain text without "," or ":". Prints nothing when stdin is
# empty. Exits 2 on a usage error.
set -euo pipefail

usage() {
  echo "usage: $0 <title> [<lines>]" >&2
  exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage
title="$1"
lines="${2:-150}"
[[ "${lines}" =~ ^[1-9][0-9]*$ ]] || usage

# awk reads all of stdin, so the command that writes it never meets a closed pipe.
awk -v title="${title}" -v lines="${lines}" '
  BEGIN { ORS = "" }
  NR <= lines {
    gsub(/\r/, "")
    gsub(/%/, "%25")
    print (NR > 1 ? "%0A" : "::error title=" title "::") $0
  }
  END { if (NR) print "\n" }'
