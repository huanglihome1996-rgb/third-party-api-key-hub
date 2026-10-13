# TikTok Shop 独立 HTTPS Callback

完整安装步骤见 [INSTALL.md](INSTALL.md)。

`cmd/ttcallback` 是独立公网回调边界，管理 Unix Socket 不会被它暴露。一个配置文件可以声明多个 App；每个 App 由 `app_id`、`app_key` 和一个 secret 引用组成。secret 只能用 `app_secret_env` 或 `app_secret_file`，不能写入 JSON。

启动前准备：

1. 为 token 加密存储准备独立的 32 字节 key 文件，权限设为 `0600`。
2. 为每个 App 设置环境变量，或创建权限为 `0600` 的 secret 文件。
3. 在 TikTok Partner Center 登记与 `redirect_url` 完全一致的 HTTPS URL。
4. 使用证书和私钥启动：

key 与 secret 文件需由服务账户 `api-key-hub` 持有，使该账户能读取。样例默认使用两个独立的 secret 文件；每个 App 的 `app_secret_env` 与 `app_secret_file` 必须二选一。

```text
ttcallback -config /etc/api-key-hub/tiktok-callback.json \
  -tls-cert /etc/letsencrypt/live/example/fullchain.pem \
  -tls-key /etc/letsencrypt/live/example/privkey.pem
```

授权入口为 `GET /authorize?app_id=<配置中的 app_id>`。服务返回授权 URL；回调路由为配置中的 `/callbacks/tiktok-shop`。服务端根据一次性 `state` 找回 App，再携带该 App 的 `app_key/app_secret` 向 TikTok 兑换 Token。state 只以哈希形式持久化，浏览器响应不会返回 code、state 或 Token。

当前本地加密文件 sink 用 AES-GCM 保护 access/refresh token；接入生产 OpenBao 时应替换 `TokenSink`，不能把 token 明文写入业务日志或普通文件。
