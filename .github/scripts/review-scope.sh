#!/usr/bin/env bash
# Decides whether an automated PR review should run, and which commits it covers.
#
# Every review comment ends with a state marker:
#
#   <!-- <marker> sha=<reviewed head> round=<n> -->
#
# The next run reads that marker and scopes the review to the commits added since,
# so code that already survived a round is not re-sampled into a fresh set of
# findings. Without this, each push re-reviews the whole diff and a non-determin-
# istic reviewer keeps surfacing a different subset of it.
#
# Environment:
#   GH_TOKEN, GITHUB_REPOSITORY, GITHUB_OUTPUT   provided by Actions
#   MARKER        marker name identifying this reviewer's comments
#   PR_NUMBER     pull request to review
#   HEAD_SHA      current head of the pull request
#   MAX_ROUNDS    automatic rounds allowed per PR (0 disables the cap)
#   FORCE_LABEL   label that forces a full review and resets the round counter,
#                 honoured once per application (see below)
set -euo pipefail

out() { printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"; }

out_multiline() {
  local name=$1 value=$2 delim="EOF_MARKER_$$"
  {
    printf '%s<<%s\n' "$name" "$delim"
    printf '%s\n' "$value"
    printf '%s\n' "$delim"
  } >>"$GITHUB_OUTPUT"
}

skip() {
  out should-review false
  echo "::notice::$1"
  exit 0
}

comments_json="${RUNNER_TEMP:-/tmp}/review-comments.json"
gh api --paginate "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" --jq '.[]' |
  jq -s '.' >"$comments_json"

# Only markers written by a bot are trusted. A human comment carrying the marker
# would otherwise let anyone with comment access move the review baseline forward
# and hide the commits in between.
prior=$(jq -c --arg m "<!-- $MARKER " '
  [ .[] | select(.user.type == "Bot" and (.body | contains($m))) ] | last // empty
' "$comments_json")

last_sha=""
last_round=0
prior_body=""
prior_created_at=""
if [ -n "$prior" ]; then
  prior_body=$(jq -r '.body' <<<"$prior")
  prior_created_at=$(jq -r '.created_at' <<<"$prior")
  marker_line=$(printf '%s\n' "$prior_body" |
    grep -o "<!-- $MARKER sha=[0-9a-f]\{7,40\} round=[0-9]\{1,3\} -->" | tail -n1 || true)
  if [ -n "$marker_line" ]; then
    last_sha=$(printf '%s' "$marker_line" | sed -n 's/.*sha=\([0-9a-f]*\).*/\1/p')
    last_round=$(printf '%s' "$marker_line" | sed -n 's/.*round=\([0-9]*\).*/\1/p')
  fi
fi

# A forced review is requested by applying FORCE_LABEL and is consumed by the next
# review this reviewer posts. Consuming it by presence alone would force a full
# review on every later push, and consuming it by removing the label would race the
# other review workflow, which watches the same label on the same events.
timestamp() { printf '%s' "$1" | tr -cd '0-9'; }

if [ -n "$prior_created_at" ]; then
  forced_at=$(gh api --paginate "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/timeline" \
    --jq '.[] | select(.event == "labeled") | [.label.name, .created_at] | @tsv' |
    awk -F'\t' -v label="$FORCE_LABEL" '$1 == label { print $2 }' | sort | tail -n1)
  if [ -n "$forced_at" ] &&
    [ "$(timestamp "$forced_at")" -gt "$(timestamp "$prior_created_at")" ]; then
    echo "::notice::'$FORCE_LABEL' was applied after the last review -- running a full review and resetting the round counter."
    last_sha=""
    last_round=0
  fi
fi

round=$((last_round + 1))
if [ "$MAX_ROUNDS" -gt 0 ] && [ "$round" -gt "$MAX_ROUNDS" ]; then
  skip "Automatic review stopped after $MAX_ROUNDS rounds on this pull request. Add the '$FORCE_LABEL' label to request another full review."
fi

diff_base=""
if [ -n "$last_sha" ]; then
  if compare=$(gh api "repos/$GITHUB_REPOSITORY/compare/$last_sha...$HEAD_SHA" 2>/dev/null); then
    case "$(jq -r '.status' <<<"$compare")" in
      identical | behind)
        skip "No commits added since the last review ($last_sha)."
        ;;
      ahead)
        diff_base=$last_sha
        ;;
      *)
        # Rebase or force-push: the earlier baseline no longer describes a prefix
        # of this branch, so an incremental range would be misleading.
        echo "::notice::History was rewritten since the last review -- falling back to a full review."
        ;;
    esac
  else
    echo "::notice::Commit $last_sha is no longer reachable -- falling back to a full review."
  fi
fi

if [ -n "$diff_base" ]; then
  scope=$(
    cat <<EOF
Earlier rounds already reviewed this pull request up to commit $diff_base. Those
commits are settled: do not re-review them, and do not report findings in them.

The diff you must review -- only the commits added since that point -- is in
\`.review-scope/review.diff\` in the checkout.

Read the full pull request diff (\`gh pr diff $PR_NUMBER\`) and any file in the
repository for the context you need to judge those commits, but report a finding
only when it is anchored to a line changed in \`.review-scope/review.diff\`. Code
an earlier round left unremarked is out of scope, even if you would flag it now.

Ignore the \`.review-scope/\` directory itself: it is scaffolding this workflow
writes into the checkout, not part of the pull request.
EOF
  )
else
  scope=$(
    cat <<EOF
No round has reviewed this pull request yet. Its full diff is in
\`.review-scope/review.diff\` in the checkout.

Ignore the \`.review-scope/\` directory itself: it is scaffolding this workflow
writes into the checkout, not part of the pull request.
EOF
  )
fi

prior_block=""
if [ -n "$prior_body" ]; then
  prior_text=$(printf '%s\n' "$prior_body" | grep -v "<!-- $MARKER " || true)
  prior_block=$(
    cat <<EOF
The previous round reported the findings below. Treat them as addressed unless the
problem is still present in the current code. Do not repeat them, and do not go
looking for replacement findings because these are gone.

<previous-round-findings>
$prior_text
</previous-round-findings>
EOF
  )
fi

# Collapse superseded review comments so the thread shows one live review.
jq -r --arg m "<!-- $MARKER " '
  .[] | select(.user.type == "Bot" and (.body | contains($m))) | .node_id
' "$comments_json" | while read -r node_id; do
  [ -n "$node_id" ] || continue
  gh api graphql -F id="$node_id" -f query='
    mutation($id: ID!) {
      minimizeComment(input: { subjectId: $id, classifier: OUTDATED }) {
        minimizedComment { isMinimized }
      }
    }' >/dev/null 2>&1 ||
    echo "::warning::Could not collapse previous review comment $node_id"
done

out should-review true
out round "$round"
out diff-base "$diff_base"
out marker-line "<!-- $MARKER sha=$HEAD_SHA round=$round -->"
out_multiline scope-instructions "$scope"
out_multiline prior-findings "$prior_block"
