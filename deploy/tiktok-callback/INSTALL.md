# TikTok Shop Callback 安装部署手册

本文部署独立的 `ttcallback` HTTPS 服务。它只承接授权入口和 TikTok 回调，不暴露 `hubd` 的 Unix Socket 管理接口。

当前实现适用于 Ubuntu 24.04 LTS、systemd 和 `linux/amd64` 或 `linux/arm64`。服务支持在一个配置文件中登记多个 App，每个 App 独立使用自己的 `app_key` 和 `app_secret`。

部署顺序：上传并解压发布包 → 安装程序和配置 → 填入各 App 信息、secret 和 TLS 证书 → 启动 systemd 服务 → 配置域名和公网 443 入口 → 在 TikTok 后台登记 Redirect URL → 完成一次真实授权。服务器无需安装 Go 或 Docker；仅启动 `ttcallback` 即可接收回调。

## 1. 部署前准备

准备以下信息：

- 一个解析到服务器的回调域名，例如 `tt-callback.example.com`；
- TikTok Partner Center 中每个 App 的 `app_key`、`service_id` 和 `app_secret`；
- 与 `https://tt-callback.example.com/callbacks/tiktok-shop` 完全一致的 Redirect URL；
- 服务器到 TikTok 授权和 Token 地址的出站 HTTPS；
- TLS 证书和私钥；
- 一个独立的 32 字节 token 加密 key。

不要把真实 App secret、Token、私钥或加密 key 写入 Git、配置样例或聊天记录。

## 2. 获取发布包

按服务器架构上传对应归档（`uname -m` 为 `x86_64` 时选 `amd64`，为 `aarch64` 时选 `arm64`）及配套 `SHA256SUMS`，先校验压缩包，再解包校验内部文件。以下以 amd64 为例：

```bash
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf api-key-hub-0.3.0-linux-amd64.tar.gz
cd api-key-hub-0.3.0-linux-amd64
sha256sum -c SHA256SUMS
```

下面的命令都从这个解包目录执行。`bin/ttcallback` 是独立 callback 程序；包中的 `hub`、`hubd` 目前用于 fixture 核心，不是 callback 的运行依赖。

## 3. 创建账户和目录

以下命令在目标 Ubuntu 主机以 root 执行：

```bash
id api-key-hub >/dev/null 2>&1 || useradd --system --user-group --home-dir / --shell /usr/sbin/nologin api-key-hub
install -d -o root -g root -m 0755 /opt/api-key-hub/bin
install -d -o root -g api-key-hub -m 0750 /etc/api-key-hub
install -d -o root -g api-key-hub -m 0750 /etc/api-key-hub/secrets
install -d -o api-key-hub -g api-key-hub -m 0700 /var/lib/api-key-hub
install -o root -g root -m 0755 bin/ttcallback /opt/api-key-hub/bin/ttcallback
```

## 4. 创建加密 key 和 App secret

仅在首次安装且 key 不存在时生成独立的 32 字节 AES-GCM key。该 key 丢失后不能解密已有 Token，升级时必须保留原 key。程序要求 key 和 App secret 文件禁止组用户及其他用户访问，因此必须由服务用户持有并设置为 `0600`（不是 `0640`）：

```bash
if [ ! -e /etc/api-key-hub/tiktok-token-store.key ]; then
  (umask 077; openssl rand -out /etc/api-key-hub/tiktok-token-store.key 32)
fi
chown api-key-hub:api-key-hub /etc/api-key-hub/tiktok-token-store.key
chmod 0600 /etc/api-key-hub/tiktok-token-store.key
```

生产环境建议每个 App 使用独立 secret 文件：

```bash
install -o api-key-hub -g api-key-hub -m 0600 /secure/source/tt-us-main.secret \
  /etc/api-key-hub/secrets/tt-us-main.secret
install -o api-key-hub -g api-key-hub -m 0600 /secure/source/tt-us-secondary.secret \
  /etc/api-key-hub/secrets/tt-us-secondary.secret
```

将每个 App 的真实 secret 放入对应文件，文件内容仅为 secret 本身。配置中的 `app_secret_file` 引用这些文件。也可以把某个 App 的 `app_secret_file` 字段替换为 `"app_secret_env": "TTS_APP_SECRET_TT_US_MAIN"`；两个字段必须二选一，不能同时存在。不要把 secret 写入 systemd unit 的 `ExecStart` 参数。

如果配置使用 `app_secret_env`，创建权限为 `0600` 的环境文件，并由受控密钥注入流程写入变量；全部使用 `app_secret_file` 时可以不创建它：

```bash
install -o root -g api-key-hub -m 0600 /dev/null /etc/api-key-hub/tiktok-callback.env
# 由受控密钥注入流程写入：TTS_APP_SECRET_TT_US_MAIN=<真实 secret>
```

## 5. 安装配置

复制样例并编辑：

```bash
install -o root -g api-key-hub -m 0640 \
  deploy/tiktok-callback/config.example.json \
  /etc/api-key-hub/tiktok-callback.json
```

至少修改每个 App 的以下字段：

```json
{
  "app_key": "真实 app key",
  "app_secret_file": "/etc/api-key-hub/secrets/tt-us-main.secret",
  "authorize_params": {"service_id": "真实 service id"},
  "authorize_url": "https://services.us.tiktokshop.com/open/authorize",
  "token_url": "https://auth.tiktok-shops.com/api/v2/token/get",
  "redirect_url": "https://tt-callback.example.com/callbacks/tiktok-shop"
}
```

多个 App 放在同一个 `apps` 对象中，键就是服务内部使用的 `app_id`，例如 `tt-us-main` 和 `tt-us-secondary`。所有 App 可以共用同一个 callback URL；服务通过一次性 `state` 找回发起授权的 App。

样例中的两个 App 默认都引用 secret 文件。只使用一个 App 时删除另一个示例条目；新增 App 时增加独立条目和 secret 文件。保存配置或更新 secret 后执行 `systemctl restart api-key-hub-ttcallback` 才会重新加载。授权和 Token 地址按 App 所属地区核对，样例授权入口为 US。

检查配置文件权限和 JSON 格式：

```bash
chown root:api-key-hub /etc/api-key-hub/tiktok-callback.json
chmod 0640 /etc/api-key-hub/tiktok-callback.json
python3 -m json.tool /etc/api-key-hub/tiktok-callback.json >/dev/null
```

## 6. 安装 TLS 证书

`ttcallback` 自己终止 TLS。让服务账户能够读取私钥，但不要让普通用户读取：

```bash
install -d -o root -g api-key-hub -m 0750 /etc/api-key-hub/tls
install -o root -g api-key-hub -m 0640 \
  /path/to/fullchain.pem /etc/api-key-hub/tls/fullchain.pem
install -o root -g api-key-hub -m 0640 \
  /path/to/privkey.pem /etc/api-key-hub/tls/privkey.pem
```

证书续期后必须重新复制证书并重启服务。若使用 Certbot，可在 deploy hook 中执行这两步和 `systemctl reload-or-restart api-key-hub-ttcallback`。

将域名的 A/AAAA 记录解析到服务器或入口负载均衡器，并开放其公网 443。默认服务仅监听 `127.0.0.1:9080`，因此仅配置 DNS 不够；还需将公网 443 转发到此 HTTPS 端口。以下 Nginx `stream` 示例适合独占公网 443 的 TCP 转发，放在 nginx.conf 的主上下文（不能放在 `http` 块或普通 HTTP `sites-enabled` 中），Nginx 需启用 stream 模块：

```nginx
stream {
    server {
        listen 443;
        proxy_pass 127.0.0.1:9080;
    }
}
```

Nginx TCP 转发与服务需位于同一主机才能使用上述回环地址。远端负载均衡器应转发到受防火墙限制的服务器内网监听地址。若使用 HTTP 反向代理而非 TCP 转发，上游必须是 `https://127.0.0.1:9080`，因为 callback 不提供明文 HTTP。

也可直接由服务监听公网 443：将 `listen_addr` 改为 `0.0.0.0:443`，创建以下 systemd 覆盖配置，使非 root 服务能够绑定 443：

```bash
install -d -o root -g root -m 0755 /etc/systemd/system/api-key-hub-ttcallback.service.d
cat > /etc/systemd/system/api-key-hub-ttcallback.service.d/https-port.conf <<'EOF'
[Service]
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
EOF
```

该配置在第 7 节的 `daemon-reload` 和首次启动时生效；已经运行的服务还需重启。直接监听 443 时无需上述 Nginx 转发；确认 443 未被其他服务占用，并开放相应防火墙/云安全组。`0.0.0.0` 仅监听 IPv4，使用此方案时域名先配置 A 记录；如配置 AAAA 记录，还需对应的 IPv6 监听或入口转发。

## 7. 安装并启动 systemd

复制 unit，并确认其中的证书路径与二进制路径正确：

```bash
install -o root -g root -m 0644 \
  deploy/tiktok-callback/api-key-hub-ttcallback.service \
  /etc/systemd/system/api-key-hub-ttcallback.service
systemctl daemon-reload
systemctl enable --now api-key-hub-ttcallback
```

检查启动状态和日志：

```bash
systemctl status api-key-hub-ttcallback --no-pager
journalctl -u api-key-hub-ttcallback -n 100 --no-pager
```

服务只应绑定配置中的 callback 地址：

```bash
ss -ltnp | grep -E ':9080|:443'
```

## 8. 部署验证

先验证健康检查：

```bash
curl --fail --silent --show-error \
  https://tt-callback.example.com/healthz
```

然后为一个 App 请求授权 URL：

```bash
curl --fail --silent --show-error \
  'https://tt-callback.example.com/authorize?app_id=tt-us-main'
```

响应应包含 `authorization_url`，但不能包含 `app_secret`、Token 或上游原始响应。浏览器完成 TikTok 授权后，TikTok 应回调：

```text
https://tt-callback.example.com/callbacks/tiktok-shop
```

回调成功页面只显示通用成功信息。实际 access token 和 refresh token 写入配置中的加密文件：

```text
/var/lib/api-key-hub/tiktok-tokens.enc.json
```

state 文件只保存 hash 和 App 绑定：

```text
/var/lib/api-key-hub/tiktok-states.json
```

## 9. 备份、升级和回退

至少独立备份以下内容，并限制备份访问权限：

- `/etc/api-key-hub/tiktok-callback.json`；
- `/etc/api-key-hub/secrets/`；
- `/etc/api-key-hub/tiktok-callback.env`（使用环境变量 secret 时）；
- `/etc/api-key-hub/tiktok-token-store.key`；
- `/var/lib/api-key-hub/tiktok-tokens.enc.json`；
- `/var/lib/api-key-hub/tiktok-states.json`。

备份和恢复必须同时保留 token 加密 key；只恢复 Token 文件而不恢复 key 没有意义。升级时从新包解压目录执行以下步骤，保留现有配置、secret、TLS 和加密 key：

```bash
cp -a /opt/api-key-hub/bin/ttcallback /opt/api-key-hub/bin/ttcallback.previous
systemctl stop api-key-hub-ttcallback
install -o root -g root -m 0755 bin/ttcallback /opt/api-key-hub/bin/ttcallback
systemctl start api-key-hub-ttcallback
systemctl is-active --quiet api-key-hub-ttcallback
curl --fail --silent https://tt-callback.example.com/healthz
```

回退时恢复上一版二进制，保留当前配置、state、Token 文件和加密 key，随后重新执行健康检查。

## 10. 当前验收边界

本手册覆盖 callback 进程、TLS、systemd、多 App secret 加载、state 持久化和本地加密 Token 文件。当前仓库尚未把该 callback 的 `TokenSink` 接入 `hubd` 的生产 OpenBao/凭据域，也没有替真实 TikTok 账号完成联调；上线前必须在受控环境补做这两项验收。
