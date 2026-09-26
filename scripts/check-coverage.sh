#!/usr/bin/env bash
# Fails if any package with statements is below the coverage threshold
# (AGENTS.md §Coverage). A package with code but no test files counts as 0%.
# Usage: scripts/check-coverage.sh [threshold]
set -euo pipefail
threshold="${1:-90}"
go test -cover ./... | awk -v min="$threshold" '
  {
    pkg = ""
    for (i = 1; i <= NF; i++) if ($i ~ /\//) { pkg = $i; break }
  }
  /\[no test files\]/ { printf "FAIL %s: no test files (0%%) < %s%%\n", pkg, min; bad = 1; next }
  /coverage: [0-9.]+% of statements/ {
    for (i = 1; i <= NF; i++) if ($i == "coverage:") pct = $(i + 1)
    sub("%", "", pct)
    if (pct + 0 < min) { printf "FAIL %s: %s%% < %s%%\n", pkg, pct, min; bad = 1 }
    else printf "ok   %s: %s%%\n", pkg, pct
  }
  END { exit bad }'
