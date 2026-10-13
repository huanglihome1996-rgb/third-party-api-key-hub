#!/bin/bash
set -euo pipefail

if [[ " ${*} " == *" --help "* || " ${*} " == *" -h "* ]]; then
    cat <<'HELP'
用法：sudo bash deploy/tiktok-callback/install.sh [选项]

Ubuntu 24.04/systemd 一次安装并启动 HTTP callback，公网 HTTPS 由外部 ALB 终止。
交互提供域名、app_id、app_key 和隐藏 app_secret，可继续添加多个 App。
安装后将 ALB 转发到服务器 HTTP 9080 端口，健康检查路径 /healthz。
  --binary PATH       指定已有支持 -http 的 Linux ttcallback
  --listen-addr ADDR  HTTP IPv4:端口（默认0.0.0.0:9080，端口>=1024）
  --service-id ID     可选 Seller service_id，不默认采用 app_id
HELP
    exit 0
fi
if [[ ${EUID} -ne 0 ]]; then
    echo '请使用 sudo bash install.sh 执行。' >&2
    exit 1
fi
if [[ ! -r /etc/os-release ]]; then
    echo '需要 Ubuntu 24.04。' >&2
    exit 1
fi
# shellcheck source=/dev/null
source /etc/os-release
if [[ ${ID:-} != ubuntu || ${VERSION_ID:-} != 24.04 || ! -d /run/systemd/system ]]; then
    echo '自动安装仅支持运行 systemd 的 Ubuntu 24.04。' >&2
    exit 1
fi
packages=()
command -v python3 >/dev/null 2>&1 || packages+=(python3)
command -v ss >/dev/null 2>&1 || packages+=(iproute2)
if (( ${#packages[@]} )); then
    apt-get update
    apt-get install -y "${packages[@]}"
fi
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec python3 "$script_dir/install.py" "$@"
