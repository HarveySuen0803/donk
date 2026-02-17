#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
APP_NAME="donk"

mkdir -p "${DIST_DIR}"

build_one() {
  local goos="$1"
  local goarch="$2"
  local output="${DIST_DIR}/${APP_NAME}-${goos}-${goarch}"

  echo "building ${goos}/${goarch} -> ${output}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -o "${output}" "${ROOT_DIR}"
}

build_all() {
  rm -f \
    "${DIST_DIR}/${APP_NAME}-darwin-amd64" \
    "${DIST_DIR}/${APP_NAME}-darwin-arm64" \
    "${DIST_DIR}/${APP_NAME}-linux-amd64" \
    "${DIST_DIR}/${APP_NAME}-linux-arm64" \
    "${DIST_DIR}/${APP_NAME}-windows-amd64" \
    "${DIST_DIR}/${APP_NAME}-windows-arm64" \
    "${DIST_DIR}/${APP_NAME}-windows-amd64.exe" \
    "${DIST_DIR}/${APP_NAME}-windows-arm64.exe"

  build_one darwin amd64
  build_one darwin arm64
  build_one linux amd64
  build_one linux arm64
  build_one windows amd64
  build_one windows arm64
}

build_single() {
  local goos="$1"
  local goarch="$2"
  local output="${DIST_DIR}/${APP_NAME}-${goos}-${goarch}"

  rm -f "${output}" "${output}.exe"
  build_one "${goos}" "${goarch}"
}

usage() {
  echo "Usage:"
  echo "  $0               # build all targets"
  echo "  $0 <goos> <arch> # build a single target, e.g. $0 darwin amd64"
}

case "$#" in
  0)
    build_all
    ;;
  2)
    build_single "$1" "$2"
    ;;
  *)
    usage
    exit 1
    ;;
esac

echo "build completed. outputs are in ${DIST_DIR}"
