#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
OUT_DIR="$ROOT_DIR/evaluation_results"

mkdir -p "$OUT_DIR"

{
  echo "=== Environment Info ==="
  date -Iseconds
  uname -a
  go version
  if command -v lscpu >/dev/null 2>&1; then
    lscpu | grep -E "Model name|CPU\(s\)|Thread|Core" || true
  fi
} > "$OUT_DIR/environment.txt"

echo "[1/4] Running correctness tests..."
(cd "$BACKEND_DIR" && go test -v ./cryptoutil -run TestComparisonCorrectness)

echo "[2/4] Running Go microbenchmarks..."
(cd "$BACKEND_DIR" && go test -bench=BenchmarkScheme -benchmem -count=3 -benchtime=500ms ./cryptoutil | tee "$OUT_DIR/microbench.txt")

echo "[3/4] Running crypto scalability suite..."
(cd "$BACKEND_DIR" && go run ./cmd/hebench -mode=crypto -voters=10,50,100,500 -candidates=5 -runs=3 -out="$OUT_DIR")

echo "[4/4] Running E2E HTTP + PostgreSQL suite (if DB is reachable)..."
if (cd "$BACKEND_DIR" && go run ./cmd/hebench -mode=e2e -voters=10,50,100 -candidates=5 -concurrency=10 -out="$OUT_DIR" 2>"$OUT_DIR/e2e_status.log"); then
  echo "E2E suite completed."
else
  echo "E2E suite skipped (PostgreSQL not reachable; see $OUT_DIR/e2e_status.log)."
fi

echo "Done. Results saved in: $OUT_DIR"
