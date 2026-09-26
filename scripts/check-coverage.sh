#!/usr/bin/env bash
# Fails if any package with statements is below the coverage threshold
# (AGENTS.md §Coverage). Usage: scripts/check-coverage.sh [threshold]
set -euo pipefail
threshold="${1:-90}"
go test -cover ./... | awk -v min="$threshold" '
  /coverage: [0-9.]+% of statements/ {
    for (i = 1; i <= NF; i++) if ($i == "coverage:") pct = $(i + 1)
    sub("%", "", pct)
    if (pct + 0 < min) { printf "FAIL %s: %s%% < %s%%\n", $2, pct, min; bad = 1 }
    else printf "ok   %s: %s%%\n", $2, pct
  }
  END { exit bad }'
