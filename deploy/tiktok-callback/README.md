# TikTok Shop 独立 Callback

在 v0.3.1 安装包解压目录执行：

```bash
sudo bash deploy/tiktok-callback/install.sh
```

按提示输入公网 callback 域名、app_id、app_key、隐藏的 app_secret，可连续添加多个 App。脚本配置并启动 HTTP `0.0.0.0:9080` 服务；HTTPS 证书由外部 ALB 管理，ALB 转发 HTTP 到服务器9080，健康检查使用 `/healthz`。完整说明见 [INSTALL.md](INSTALL.md)。

公网 Redirect URL 为 `https://你的域名/callbacks/tiktok-shop`。需要发起 US Seller 授权时，执行安装命令时添加 `--service-id '实际service_id'`；app_id 是配置索引，不能代替 service_id。

`cmd/ttcallback` 提供授权入口、一次性 state 和服务端 Token 兑换，不暴露 hubd 的 Unix Socket 管理接口。每个 App 使用独立 `app_key` 与 secret 引用；secret 支持 `app_secret_file` 或 `app_secret_env` 二选一，不能直接写入 JSON。安装脚本使用0600独立文件。

脚本保留已有 App、加密 key 和 Token/state。Token 文件使用 AES-GCM 加密；加密 key 必须单独妥善备份。当前 TokenSink 尚未接入生产 OpenBao。

高级部署可使用 `ttcallback -config <配置路径> -http` 启动 HTTP；默认 TLS 模式需要 `-tls-cert` 和 `-tls-key`，不能与 `-http` 同用。
