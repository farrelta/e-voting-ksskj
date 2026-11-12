#!/usr/bin/env sh
# Builds the browser encryption module (Go -> WebAssembly) into the frontend's
# public folder. Run it once before `npm run dev` / `npm run build`, and again
# whenever backend/cryptoutil or backend/cmd/clientcrypto changes.
#
# Needs Go 1.24+ and network access to download Go modules on the first run.
# The `docker compose` build does this automatically (see frontend/Dockerfile).
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/frontend/public/crypto"
mkdir -p "$out"

cd "$root/backend"
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$out/clientcrypto.wasm" ./cmd/clientcrypto

# wasm_exec.js must come from the same Go version that built the .wasm file.
goroot="$(go env GOROOT)"
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
  cp "$goroot/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
else
  cp "$goroot/misc/wasm/wasm_exec.js" "$out/wasm_exec.js"
fi

echo "Built $out/clientcrypto.wasm and wasm_exec.js"
