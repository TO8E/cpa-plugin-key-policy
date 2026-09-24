#!/bin/sh
# Build locally with native Go and, on macOS, Zig as the Linux cross compiler.
set -eu
cd "$(dirname "$0")/.."
REPO_ROOT=$(pwd)
BUILD_WORK=${BUILD_WORK:-"$REPO_ROOT/.local-build"}
GO_BIN=${GO_BIN:-go}
ZIG_BIN=${ZIG_BIN:-zig}
mkdir -p "$BUILD_WORK" dist
export GOTOOLCHAIN=local
export GOPATH="$BUILD_WORK/gopath" GOCACHE="$BUILD_WORK/gocache"
export ZIG_GLOBAL_CACHE_DIR="$BUILD_WORK/zig-cache"

# The tracked HTML is a placeholder; embed the actual UI only in the binaries.
cp internal/plugin/web/dist/index.html "$BUILD_WORK/index.original.html"
trap 'cp "$BUILD_WORK/index.original.html" internal/plugin/web/dist/index.html' EXIT
npm ci --prefix web
VITE_HOSTED=1 npm run build --prefix web
cp web/dist/index.html internal/plugin/web/dist/index.html
test -z "$("$(dirname "$GO_BIN")/gofmt" -l internal/plugin/app.go internal/plugin/types.go internal/plugin/scheduler_recovery_test.go internal/plugin/scheduler_test.go)"
"$GO_BIN" test -race ./...
"$GO_BIN" vet ./...

case "$(uname -s)" in
  Darwin)
    # Check the C ABI locally using a native library built from the same source.
    CGO_ENABLED=1 "$GO_BIN" build -trimpath -buildvcs=false -tags cshared \
      -buildmode=c-shared -o "$BUILD_WORK/cpa-key-policy.dylib" ./cmd/cpa-key-policy
    python3 scripts/smoke-shared-library.py "$BUILD_WORK/cpa-key-policy.dylib"
    ZIG_BIN=$(command -v "$ZIG_BIN")
    export ZIG_BIN
    cat > "$BUILD_WORK/zig-cc" <<'CC'
#!/bin/sh
exec "$ZIG_BIN" cc -target x86_64-linux-gnu.2.31 "$@"
CC
    chmod +x "$BUILD_WORK/zig-cc"
    export CC="$BUILD_WORK/zig-cc"
    ;;
  Linux) : ;;
  *) echo 'Run this script on macOS or Linux' >&2; exit 1 ;;
esac

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GOCACHE="$BUILD_WORK/gocache-linux" \
  "$GO_BIN" build -trimpath -buildvcs=false -tags cshared -buildmode=c-shared \
  -ldflags='-s -w' -o dist/cpa-key-policy.so ./cmd/cpa-key-policy
"$GO_BIN" run scripts/inspect-linux.go dist/cpa-key-policy.so > dist/BUILD.txt
printf 'Source commit: %s\n' "$(git rev-parse HEAD)" >> dist/BUILD.txt
"$GO_BIN" version >> dist/BUILD.txt
"$GO_BIN" version -m dist/cpa-key-policy.so >> dist/BUILD.txt
if [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ]; then
  python3 scripts/smoke-shared-library.py dist/cpa-key-policy.so
fi
cd dist
shasum -a 256 cpa-key-policy.so > SHA256SUMS
zip cpa-key-policy_linux_amd64.zip cpa-key-policy.so SHA256SUMS BUILD.txt
unzip -t cpa-key-policy_linux_amd64.zip
unzip -p cpa-key-policy_linux_amd64.zip cpa-key-policy.so > "$BUILD_WORK/packaged.so"
cmp cpa-key-policy.so "$BUILD_WORK/packaged.so"
shasum -a 256 -c SHA256SUMS
