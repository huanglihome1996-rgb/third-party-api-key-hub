#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
version="${HUB_RELEASE_VERSION:-0.3.0}"
[[ "$version" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,63}$ ]] || exit 1
name="api-key-hub-${version}-linux-amd64"
archive="dist/releases/$name.tar.gz"
[[ -f "$archive" ]] || { echo 'build release archives first' >&2; exit 1; }
mkdir -p dist/operations-image
tar -xzf "$archive" -C dist/operations-image "$name/bin/hub" "$name/bin/hubd" "$name/tools"
cat > dist/operations-image/Dockerfile <<DOCKER
FROM python:3.13-slim-bookworm
RUN groupadd --gid 10001 hub && useradd --uid 10001 --gid hub --no-create-home hub && mkdir /reports /state && chown hub:hub /reports /state && chmod 700 /state
COPY $name/bin/hub $name/bin/hubd /usr/local/bin/
COPY $name/tools /tools
USER 10001:10001
ENTRYPOINT ["python", "/tools/benchmark.py"]
DOCKER
