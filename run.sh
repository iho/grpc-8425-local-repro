#!/usr/bin/env bash
# Local repro: grpc-go#8425 transport.Close hang vs #8534 fix.
# Relates to kubernetes/kubernetes#140911 (release-1.34/1.35 still on grpc 1.72.x).
#
# Usage: ./run.sh
set -euo pipefail

WORKDIR=${TMPDIR:-/tmp}/grpc-8425-demo-$$
mkdir -p "$WORKDIR"
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

TEST_SRC=$(cd "$(dirname "$0")" && pwd)/close_mute_repro_test.go

run_tag() {
  local tag=$1
  local dir="$WORKDIR/grpc-go-$tag"
  echo "============================================================"
  echo " google.golang.org/grpc $tag"
  echo "============================================================"
  git clone --quiet --depth 1 --branch "$tag" https://github.com/grpc/grpc-go.git "$dir"
  cp "$TEST_SRC" "$dir/internal/transport/close_mute_repro_test.go"
  # -count=1: no cache; test may fail on vulnerable versions (that IS the signal)
  set +e
  (cd "$dir" && go test ./internal/transport/ -run TestMutePeerCloseLatency -v -count=1)
  local rc=$?
  set -e
  echo "exit=$rc"
  echo
  return 0
}

echo "Mute peer: Read blocks after HTTP/2 SETTINGS; Close() does not unblock Read."
echo "Only SetReadDeadline unblocks Read (what #8534 adds in http2Client.Close)."
echo
echo "Expect:"
echo "  v1.72.2  -> Close BLOCKS  (vulnerable; same generation as k8s release-1.34/1.35)"
echo "  v1.79.3  -> Close ~1s     (fixed; same pin as this PR / release-1.36)"
echo

run_tag v1.72.2
run_tag v1.79.3

echo "Done. Vulnerable = hang on Close; fixed = ~1s return via SetReadDeadline."
