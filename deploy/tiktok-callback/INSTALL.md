# TikTok Callback 安装：外部 ALB + HTTP 服务

适用于 Ubuntu 24.04/systemd、amd64 或 arm64。ALB 负责公网 HTTPS 和证书，callback 在服务器监听 HTTP `9080`。

## 一次安装

下载并解压 v0.3.1 安装包，在解压目录执行：

```bash
sudo bash deploy/tiktok-callback/install.sh
```

按提示输入四项：

- 公网 callback 域名，例如 `tt-callback.example.com`；也接受 `https://tt-callback.example.com`。
- `app_id`：本服务中区分 App 的标识，例如 `tt-us-main`，也可使用数字 ID。
- `app_key`。
- `app_secret`：隐藏输入，不会写入命令历史或显示在日志中。

可以继续录入多个 App。脚本自动创建独立 secret 文件、JSON 配置和 Token 加密 key，安装程序与 systemd 服务，设置开机启动并检查 HTTP 健康状态。重跑时按 `app_id` 更新，保留其他 App 和已有 key、Token/state；安装失败尝试恢复原文件与运行状态，数据保护规则见下文。

安装器不申请或配置 HTTPS 证书，也不需要本机80/443。目标服务器无需 Go 或 Docker；缺少 Python3 或 iproute2 时脚本会自动安装。

如需其他监听地址或端口：

```bash
sudo bash deploy/tiktok-callback/install.sh --listen-addr 0.0.0.0:9081
```

支持 IPv4 和 `1024–65535` 端口。已有自定义存储路径、unit 或未知 drop-in 时会拒绝自动接管并说明原因。

## ALB 转发

配置以下对应关系：

| 项目 | 配置 |
| --- | --- |
| 公网 listener | HTTPS 443，使用你管理的证书 |
| target group | HTTP → 服务器内网 IP:9080 |
| 健康检查 | HTTP `/healthz`，成功码200 |
| DNS | callback 域名指向 ALB |
| 服务器入站 | 允许 ALB 访问9080 |
| TikTok Redirect URL | `https://你的域名/callbacks/tiktok-shop` |

使用自定义端口时同步修改 target group。ALB 应转发授权和回调的完整路径及查询参数。公网域名可以在服务安装完成后配置；本地健康检查不依赖 ALB 或 DNS。

安装后验证：

```bash
curl --fail http://127.0.0.1:9080/healthz
curl --fail https://你的域名/healthz
sudo systemctl status api-key-hub-ttcallback --no-pager
```

第一条验证本机服务，第二条在 ALB/DNS 配置好后验证公网链路。日志：

```bash
sudo journalctl -u api-key-hub-ttcallback -n 50 --no-pager
```

## Seller 授权

上述四项信息足够启动 callback。由本服务发起 TikTok Seller 授权时，还需要 Partner Center 的 `service_id`；它与 `app_id` 不同，不能相互替代：

```bash
sudo bash deploy/tiktok-callback/install.sh --service-id '实际service_id'
```

此选项配置本次录入 App 的 US Seller 授权入口。多个 App 的 `service_id` 不同，就分次运行、每次录入一个 App。已配置 `service_id` 时脚本会输出授权入口，例如：

```text
https://你的域名/authorize?app_id=tt-us-main
```

该入口返回 `authorization_url`，打开它完成 TikTok 授权。所有 App 共用 callback URL，服务通过一次性 state 找回 App，并在服务端用该 App 的 key/secret 兑换 Token。公网回调地址始终是 HTTPS，ALB 到服务之间使用 HTTP。

## 数据、升级与高级部署

发布附件含 `SHA256SUMS`。下载对应架构的归档后先校验，再解包并检查包内文件：

```bash
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf api-key-hub-0.3.1-linux-amd64.tar.gz
cd api-key-hub-0.3.1-linux-amd64
sha256sum -c SHA256SUMS
```

ARM64 服务器将归档名中的 `amd64` 替换为 `arm64`。

配置在 `/etc/api-key-hub/tiktok-callback.json`，各 App 的 secret 在 `/etc/api-key-hub/secrets/`，文件由服务账户持有、权限0600。加密 key 在 `/etc/api-key-hub/tiktok-token-store.key`，必须和 `/var/lib/api-key-hub/tiktok-tokens.enc.json` 一起备份；丢失 key 无法解密 Token。state 在 `/var/lib/api-key-hub/tiktok-states.json`。

升级时解压新版安装包并重跑安装脚本。已有 Token 而 key 缺失时，脚本中止并要求恢复原 key，不生成替代 key。域名改变后需要同步修改各 App 的 TikTok Redirect URL。

安装失败时不会回写 Token/state。若安装期间产生了新数据，脚本会把本次配置、各 App secret 和匹配的 key 保存到报错中指定的 `/var/backups/api-key-hub-ttcallback/attempt-*`，目录仅 root 可访问；恢复时应保留整套文件。首次安装产生数据后失败，会保留对应配置、secret 和 key，修复环境后可重跑。若无法确认进程已停止，脚本会保留本次文件并报告自动回滚未完成，需先检查服务状态。

高级部署可手动编辑配置中的授权区域、secret 引用和监听地址。默认程序仍支持自己终止 TLS；手动启动时提供证书参数即可：

```bash
/opt/api-key-hub/bin/ttcallback \
  -config /etc/api-key-hub/tiktok-callback.json \
  -tls-cert /实际证书路径/fullchain.pem -tls-key /实际证书路径/privkey.pem
```

直接 TLS 模式与 `-http` 参数互斥，需要同步调整 systemd 的 ExecStart。旧 v0.3.0 不支持 HTTP 模式；ALB 使用 HTTP target 时请安装 v0.3.1 或更新版本。

安装器的文件/回滚测试与本地 HTTP/HTTPS 进程测试不代表已完成用户服务器、ALB、ARM64 实机或真实 TikTok 授权验收。callback 的本地加密 Token 存储尚未接入 hubd/OpenBao。
