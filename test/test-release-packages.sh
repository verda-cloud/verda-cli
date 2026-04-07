#!/usr/bin/env bash
set -euo pipefail

DIST_DIR="${1:-dist}"
PASS=0
FAIL=0

run_test() {
  local name="$1"
  shift
  echo "=== Testing: ${name} ==="
  if "$@"; then
    echo "--- PASS: ${name}"
    ((PASS++))
  else
    echo "--- FAIL: ${name}"
    ((FAIL++))
  fi
  echo
}

# Snapshot build (skip if dist already exists)
if [ ! -d "${DIST_DIR}" ]; then
  echo "=== Building snapshot ==="
  goreleaser release --snapshot --clean
  echo
fi

# deb (Ubuntu)
run_test "deb/ubuntu" docker run --rm -v "${PWD}/${DIST_DIR}:/dist:ro" ubuntu:24.04 sh -c '
  dpkg -i /dist/verda_*_linux_amd64.deb && verda version
'

# rpm (Fedora)
run_test "rpm/fedora" docker run --rm -v "${PWD}/${DIST_DIR}:/dist:ro" fedora:41 sh -c '
  rpm -i /dist/verda_*_linux_amd64.rpm && verda version
'

# apk (Alpine)
run_test "apk/alpine" docker run --rm -v "${PWD}/${DIST_DIR}:/dist:ro" alpine:3.20 sh -c '
  apk add --allow-untrusted /dist/verda_*_linux_amd64.apk && verda version
'

# tar.gz binary
run_test "tar.gz/binary" docker run --rm -v "${PWD}/${DIST_DIR}:/dist:ro" ubuntu:24.04 sh -c '
  tar xzf /dist/verda_*_linux_amd64.tar.gz -C /usr/local/bin && verda version
'

# Homebrew (only works after a real release)
if [ "${TEST_HOMEBREW:-}" = "1" ]; then
  run_test "homebrew" docker run --rm homebrew/brew sh -c '
    brew tap verda-cloud/tap && brew install verda && verda version
  '
fi

# Scoop (skip — no good container option for Windows)

echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[ "${FAIL}" -eq 0 ]
