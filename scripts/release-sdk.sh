#!/bin/sh
set -eu

REPO_MODULE="github.com/valon-technologies/gestalt"
API_MODULE="$REPO_MODULE/sdk/pluginapi"
SDK_MODULE="$REPO_MODULE/sdk/pluginsdk"

usage() {
    echo "Usage: $0 VERSION [--dry-run]"
    echo ""
    echo "Release sdk/pluginapi and sdk/pluginsdk as Go sub-module tags."
    echo "Main branch is never modified; the pluginsdk release commit"
    echo "is created in a temporary git worktree."
    echo ""
    echo "  VERSION   Semver without leading v (e.g. 0.1.0)"
    echo "  --dry-run Show commands without executing"
    exit 1
}

VERSION=""
DRY_RUN=false

for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=true ;;
        --help|-h) usage ;;
        *)
            if [ -z "$VERSION" ]; then
                VERSION="$arg"
            else
                echo "unexpected argument: $arg" >&2
                usage
            fi
            ;;
    esac
done

if [ -z "$VERSION" ]; then
    usage
fi

if ! echo "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$'; then
    echo "error: VERSION must be semver (e.g. 0.1.0 or 0.0.0-alpha.1), got: $VERSION" >&2
    exit 1
fi

API_TAG="sdk/pluginapi/v$VERSION"
SDK_TAG="sdk/pluginsdk/v$VERSION"

run() {
    echo "+ $*"
    if [ "$DRY_RUN" = false ]; then
        "$@"
    fi
}

ORIG_DIR="$(pwd)"

if [ "$DRY_RUN" = false ] && [ -n "$(git status --porcelain)" ]; then
    echo "error: working tree is not clean" >&2
    exit 1
fi

echo "=== Step 1: Tag pluginapi ==="
run git tag "$API_TAG"
run git push origin "$API_TAG"

echo ""
echo "=== Step 2: Create worktree for pluginsdk release ==="
WORK=$(mktemp -u -d)
BRANCH="release-sdk-v$VERSION"
run git worktree add "$WORK" -b "$BRANCH" HEAD

echo ""
echo "=== Step 3: Pin pluginsdk to released pluginapi ==="
if [ "$DRY_RUN" = false ]; then
    cd "$WORK/sdk/pluginsdk"
    go mod edit -dropreplace="$API_MODULE"
    go mod edit -require="$API_MODULE@v$VERSION"
    GOPRIVATE="$REPO_MODULE" GONOSUMCHECK="$REPO_MODULE/*" go mod tidy
    cd "$WORK"
    git add sdk/pluginsdk/go.mod sdk/pluginsdk/go.sum
    git commit -m "sdk/pluginsdk: pin pluginapi v$VERSION for release"
else
    echo "+ cd $WORK/sdk/pluginsdk"
    echo "+ go mod edit -dropreplace=$API_MODULE"
    echo "+ go mod edit -require=$API_MODULE@v$VERSION"
    echo "+ GOPRIVATE=$REPO_MODULE GONOSUMCHECK=$REPO_MODULE/* go mod tidy"
    echo "+ git add sdk/pluginsdk/go.mod sdk/pluginsdk/go.sum"
    echo "+ git commit -m 'sdk/pluginsdk: pin pluginapi v$VERSION for release'"
fi

echo ""
echo "=== Step 4: Tag and push pluginsdk ==="
run git -C "$WORK" tag "$SDK_TAG"
run git push origin "$SDK_TAG"

echo ""
echo "=== Step 5: Cleanup worktree ==="
run git worktree remove "$WORK"
run git branch -D "$BRANCH"

echo ""
echo "=== Done ==="
echo ""
echo "Tags pushed:"
echo "  $API_TAG"
echo "  $SDK_TAG"
echo ""
echo "Warm the proxy cache:"
echo "  GOPROXY=proxy.golang.org go list -m $API_MODULE@v$VERSION"
echo "  GOPROXY=proxy.golang.org go list -m $SDK_MODULE@v$VERSION"
