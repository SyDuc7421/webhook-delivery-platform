#!/usr/bin/env bash
# Blocks edits to migration files that already exist on origin/main.
set -euo pipefail

base="${BASE_REF:-origin/main}"
git fetch --quiet origin main 2>/dev/null || true

fail=0
for f in "$@"; do
  if git cat-file -e "${base}:${f}" 2>/dev/null; then
    if ! git diff --quiet "${base}" -- "${f}"; then
      echo "ERROR: ${f} already exists on ${base} — migrations are append-only."
      echo "       Create a new migration with: make migrate-new name=<change>"
      fail=1
    fi
  fi
done
exit $fail
