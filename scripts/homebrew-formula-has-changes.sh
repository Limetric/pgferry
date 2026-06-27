#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <repo-dir> <formula-path>" >&2
  exit 2
fi

repo_dir="$1"
formula_path="$2"

test -n "$(git -C "${repo_dir}" status --porcelain -- "${formula_path}")"
