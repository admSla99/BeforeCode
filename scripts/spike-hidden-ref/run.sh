#!/usr/bin/env bash
# Spike for issue #4: verify the plans ref (refs/beforecode/plans) on real GitHub.
#
# Usage: scripts/spike-hidden-ref/run.sh <owner>/<scratch-repo>
#
# Use a SCRATCH repository with a default branch. The script commits two
# probe workflows to its default branch, pushes to refs/beforecode/plans, and
# opens and closes a pull request. It never deletes or force-pushes a ref.
#
# Needs: git >= 2.38 (merge-tree --write-tree), gh logged in with the repo and
# workflow scopes, and git push access to the repository.
set -euo pipefail

REPO=${1:?usage: run.sh <owner>/<scratch-repo>}
URL="https://github.com/$REPO.git"
HERE=$(cd "$(dirname "$0")" && pwd)
WORK=$(mktemp -d)
RUN=$(date -u +%Y%m%dT%H%M%SZ)
REFSPEC='+refs/beforecode/*:refs/beforecode-remote/*'
START=$(date -u +%Y-%m-%dT%H:%M:%SZ)
PUSHED=() # commits pushed to the plans ref by this script

pass() { echo "PASS $*"; }
info() { echo "INFO $*"; }
fail() { echo "FAIL $*" >&2; exit 1; }

# add_file <dir> <parent commit or empty> <path> <content> -> prints the new commit.
# Pure plumbing with a temporary index: the working tree, the real index and
# HEAD are never touched.
add_file() {
	local dir=$1 parent=$2 path=$3 content=$4 idx blob tree
	idx="$WORK/index.$RANDOM"
	if [ -n "$parent" ]; then GIT_INDEX_FILE=$idx git -C "$dir" read-tree "$parent"; fi
	blob=$(printf '%s\n' "$content" | git -C "$dir" hash-object -w --stdin)
	GIT_INDEX_FILE=$idx git -C "$dir" update-index --add --cacheinfo "100644,$blob,$path"
	tree=$(GIT_INDEX_FILE=$idx git -C "$dir" write-tree)
	rm -f "$idx"
	if [ -n "$parent" ]; then
		git -C "$dir" commit-tree "$tree" -p "$parent" -m "spike $RUN: add $path"
	else
		git -C "$dir" commit-tree "$tree" -m "spike $RUN: add $path"
	fi
}

snapshot() {
	git -C "$1" status --porcelain=v1 --branch
	git -C "$1" rev-parse HEAD
	git -C "$1" for-each-ref refs/heads refs/tags
}

echo "Spike $RUN against $REPO (work dir $WORK)"
git clone -q "$URL" "$WORK/a"
git clone -q "$URL" "$WORK/b"
# Without a configured identity, use the gh account's noreply address.
if [ -z "$(git -C "$WORK/a" config user.email || true)" ]; then
	login=$(gh api user -q .login)
	email="$(gh api user -q .id)+$login@users.noreply.github.com"
	for c in a b; do
		git -C "$WORK/$c" config user.name "$login"
		git -C "$WORK/$c" config user.email "$email"
	done
fi
DEFAULT=$(git -C "$WORK/a" symbolic-ref --short HEAD)

# Setup: probe workflows on the default branch (normal commit in a temp clone).
mkdir -p "$WORK/a/.github/workflows"
cp "$HERE/plans-ref.yml" "$HERE/ref-push-probe.yml" "$WORK/a/.github/workflows/"
git -C "$WORK/a" add .github/workflows
if ! git -C "$WORK/a" diff --cached --quiet; then
	git -C "$WORK/a" commit -q -m "Add probe workflows for the plans ref spike"
	git -C "$WORK/a" push -q origin "$DEFAULT"
	git -C "$WORK/b" pull -q
	info "installed probe workflows on $DEFAULT"
fi

# --- AC-5 (clone): a plain clone does not carry the ref -----------------------
for c in a b; do git -C "$WORK/$c" config --add remote.origin.fetch "$REFSPEC"; done
BEFORE_A=$(snapshot "$WORK/a")

# --- AC-1: push from clone a, fetch with the refspec in clone b --------------
git -C "$WORK/a" fetch -q origin
parent=$(git -C "$WORK/a" rev-parse -q --verify refs/beforecode-remote/plans || true)
if [ -z "$parent" ]; then
	parent=$(add_file "$WORK/a" "" format-version 1)
fi
c=$(add_file "$WORK/a" "$parent" "spike/$RUN/from-a.md" "written by clone a")
git -C "$WORK/a" update-ref refs/beforecode/plans "$c"
git -C "$WORK/a" push -q origin refs/beforecode/plans
PUSHED+=("$c")
[ "$(snapshot "$WORK/a")" = "$BEFORE_A" ] || fail "plumbing write changed the working tree, index or branches"
pass "plumbing write leaves git status, HEAD and branches untouched"

git clone -q "$URL" "$WORK/plain"
if git -C "$WORK/plain" for-each-ref | grep -q beforecode; then
	fail "a plain clone carried refs/beforecode"
fi
git ls-remote "$URL" refs/beforecode/plans | grep -q "$c" || fail "ls-remote does not show the pushed commit"
pass "AC-5 plain clone does not carry the ref; ls-remote shows it"
git clone -q --mirror "$URL" "$WORK/mirror"
if git -C "$WORK/mirror" for-each-ref | grep -q refs/beforecode/plans; then
	info "git clone --mirror carries refs/beforecode/plans"
else
	info "git clone --mirror does NOT carry refs/beforecode/plans"
fi

git -C "$WORK/b" fetch -q origin
got=$(git -C "$WORK/b" show "refs/beforecode-remote/plans:spike/$RUN/from-a.md")
[ "$got" = "written by clone a" ] || fail "clone b read '$got'"
pass "AC-1 pushed from clone a, fetched in clone b with $REFSPEC and read the file"

# --- AC-2: concurrent push is rejected, then fetch + merge-tree + push --------
git -C "$WORK/b" update-ref refs/beforecode/plans refs/beforecode-remote/plans
ca=$(add_file "$WORK/a" refs/beforecode/plans "spike/$RUN/concurrent-a.md" "a")
git -C "$WORK/a" update-ref refs/beforecode/plans "$ca"
cb=$(add_file "$WORK/b" refs/beforecode/plans "spike/$RUN/concurrent-b.md" "b")
git -C "$WORK/b" update-ref refs/beforecode/plans "$cb"
git -C "$WORK/a" push -q origin refs/beforecode/plans
PUSHED+=("$ca")
if git -C "$WORK/b" push origin refs/beforecode/plans 2>"$WORK/reject.txt"; then
	fail "second concurrent push was accepted"
fi
grep -Eq 'non-fast-forward|fetch first' "$WORK/reject.txt" || { cat "$WORK/reject.txt"; fail "push failed for another reason"; }
info "rejection: $(grep -E 'rejected' "$WORK/reject.txt" | head -1 | sed 's/^ *//')"
git -C "$WORK/b" fetch -q origin
tree=$(git -C "$WORK/b" merge-tree --write-tree refs/beforecode/plans refs/beforecode-remote/plans) || fail "merge-tree reported a conflict"
merge=$(git -C "$WORK/b" commit-tree "$tree" -p refs/beforecode/plans -p refs/beforecode-remote/plans -m "spike $RUN: merge")
git -C "$WORK/b" update-ref refs/beforecode/plans "$merge"
git -C "$WORK/b" push -q origin refs/beforecode/plans
PUSHED+=("$merge")
git -C "$WORK/a" fetch -q origin
for f in from-a concurrent-a concurrent-b; do
	git -C "$WORK/a" cat-file -e "refs/beforecode-remote/plans:spike/$RUN/$f.md" || fail "$f.md missing after merge"
done
pass "AC-2 second push rejected as non-fast-forward; fetch + merge-tree --write-tree + push kept both files"

# --- AC-3, AC-4: pull_request workflow reads and writes the ref ---------------
BRANCH="spike-pr-$RUN"
git -C "$WORK/a" switch -q -c "$BRANCH"
echo "$RUN" >"$WORK/a/spike-pr.txt"
git -C "$WORK/a" add spike-pr.txt
git -C "$WORK/a" commit -q -m "Trigger the plans-ref probe"
git -C "$WORK/a" push -q origin "$BRANCH"
PR=$(gh pr create -R "$REPO" --base "$DEFAULT" --head "$BRANCH" --title "Spike $RUN" --body "Triggers the plans-ref probe (BeforeCode issue #4).")
info "opened $PR"
run_id=""
for _ in $(seq 1 60); do
	run_id=$(gh run list -R "$REPO" --workflow plans-ref.yml --branch "$BRANCH" --json databaseId -q '.[0].databaseId')
	[ -n "$run_id" ] && break
	sleep 5
done
[ -n "$run_id" ] || fail "plans-ref workflow did not start"
if gh run watch -R "$REPO" "$run_id" --exit-status >/dev/null; then
	pass "AC-3 pull_request workflow fetched the ref and read format-version (run $run_id)"
	git -C "$WORK/a" fetch -q origin
	git -C "$WORK/a" cat-file -e "refs/beforecode-remote/plans:actions/$BRANCH.md" || fail "workflow file not in the ref"
	pass "AC-4 workflow pushed to the ref with GITHUB_TOKEN (contents: write); author: $(git -C "$WORK/a" log -1 --format='%an <%ae>' refs/beforecode-remote/plans)"
else
	gh run view -R "$REPO" "$run_id" --log-failed | tail -20
	fail "plans-ref workflow failed (run $run_id), see log above"
fi
gh pr close -R "$REPO" "$PR" --delete-branch >/dev/null
info "closed $PR and deleted $BRANCH"

# --- AC-5: triggers and visibility --------------------------------------------
sleep 30 # give GitHub time to start any workflow a ref push might trigger
# The run listing lags; wait until it contains the known pull_request run so an
# empty listing cannot pass the check by accident.
for _ in $(seq 1 30); do
	runs=$(gh api "repos/$REPO/actions/runs?created=%3E%3D$START&per_page=100" \
		-q '.workflow_runs[] | "\(.id)|\(.name)|\(.event)|\(.head_branch)|\(.head_sha)"')
	echo "$runs" | grep -q "^$run_id|" && break
	sleep 10
done
echo "$runs" | grep -q "^$run_id|" || fail "workflow run listing never showed run $run_id"
info "workflow runs since $START:"
echo "$runs" | sed 's/^/     /'
for sha in "${PUSHED[@]}"; do
	if echo "$runs" | grep -q "$sha"; then fail "a push of $sha to the plans ref triggered a workflow"; fi
done
pass "AC-5 pushes to refs/beforecode/plans triggered no workflow (unfiltered push/create/delete probe)"

matching=$(gh api "repos/$REPO/git/matching-refs/beforecode" -q '.[].ref')
info "GET /git/matching-refs/beforecode -> ${matching:-<empty>}"
branches=$(gh api "repos/$REPO/branches" -q '.[].name' | tr '\n' ' ')
info "GET /branches -> $branches"
tip=$(git -C "$WORK/a" rev-parse refs/beforecode-remote/plans)
code=$(curl -s -o /dev/null -w '%{http_code}' "https://github.com/$REPO/tree/$tip")
info "web UI https://github.com/$REPO/tree/$tip -> HTTP $code"
code=$(curl -s -o /dev/null -w '%{http_code}' "https://github.com/$REPO/tree/refs/beforecode/plans")
info "web UI https://github.com/$REPO/tree/refs/beforecode/plans -> HTTP $code"

echo "Done. Work dir: $WORK"
