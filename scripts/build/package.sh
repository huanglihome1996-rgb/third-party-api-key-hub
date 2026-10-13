#!/usr/bin/env bash
set -euo pipefail
umask 022
cd "$(dirname "$0")/../.."

version="${HUB_RELEASE_VERSION:-0.3.1}"
revision="${HUB_SOURCE_REVISION:-}"
if [[ -z "$revision" ]]; then revision="$(git rev-parse HEAD)"; fi
[[ "$version" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,63}$ ]] || exit 1
[[ "$revision" =~ ^([a-f0-9]{40}|[a-f0-9]{64})$ ]] || exit 1
go_version="$(awk '$1 == "go" {print $2}' go.mod | tr -d '\r')"
[[ "$(go env GOVERSION)" == "go${go_version}" ]] || { echo 'locked Go toolchain required' >&2; exit 1; }
export GOTOOLCHAIN=local
go mod download
go mod verify
mkdir -p dist/releases
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
tree_hash="$(find cmd internal -type f -name '*.go' -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d ' ' -f 1)"
for arch in amd64 arm64; do
  name="api-key-hub-${version}-linux-${arch}"
  target="$stage/$name"
  mkdir -p "$target/bin" "$target/deploy/systemd" "$target/tools"
  for binary in hub hubd ttcallback; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -buildvcs=false -trimpath -ldflags="-buildid= -X github.com/third-party-api-key-hub/internal/buildinfo.Version=$version-$tree_hash" -o "$target/bin/$binary" "./cmd/$binary"
  done
  cp deploy/systemd/* "$target/deploy/systemd/"
  mkdir -p "$target/deploy/tiktok-callback"
  for file in INSTALL.md README.md config.example.json api-key-hub-ttcallback.service install.sh install.py; do
    cp "deploy/tiktok-callback/$file" "$target/deploy/tiktok-callback/"
  done
  cp deploy/fixture-backup.json "$target/deploy/"
  cp docs/operations.md "$target/OPERATIONS.md"
  cp scripts/operations/common.py scripts/operations/benchmark.py scripts/operations/drill.py scripts/operations/capacity.py "$target/tools/"
  sha256sum go.mod go.sum > "$target/modules.sha256"
  printf '{"format":1,"mode":"fixture+ttcallback","version":"%s","source_revision":"%s","source_tree_sha256":"%s","build_version":"%s-%s","go_version":"%s","os":"linux","arch":"%s","callback_acceptance":"pending","production_acceptance":"pending"}\n' "$version" "$revision" "$tree_hash" "$version" "$tree_hash" "$go_version" "$arch" > "$target/release.json"
  find "$target" -type d -exec chmod 0755 {} +
  find "$target" -type f -exec chmod 0644 {} +
  chmod 0755 "$target/bin/hub" "$target/bin/hubd" "$target/bin/ttcallback"
  chmod 0755 "$target/deploy/tiktok-callback/install.sh" "$target/deploy/tiktok-callback/install.py"
  (cd "$target" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
  tar --sort=name --format=gnu --mtime='@0' --owner=0 --group=0 --numeric-owner -C "$stage" -cf - "$name" | gzip -n > "dist/releases/$name.tar.gz.tmp"
  mv "dist/releases/$name.tar.gz.tmp" "dist/releases/$name.tar.gz"
done
(cd dist/releases && sha256sum "api-key-hub-${version}-linux-amd64.tar.gz" "api-key-hub-${version}-linux-arm64.tar.gz" > SHA256SUMS)
cat dist/releases/SHA256SUMS
