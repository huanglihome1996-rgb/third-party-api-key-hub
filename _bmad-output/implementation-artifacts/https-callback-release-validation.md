# HTTPS Callback 发布包验证

验证日期：2026-10-13。

## 交付状态

- 已实现：独立 `ttcallback` HTTPS 进程、多 App 配置、secret 文件或环境变量加载、一次性 state、服务端 Token 兑换及 AES-GCM 文件保存。
- 已打包：`0.3.0` Linux amd64/arm64 归档，包含三个二进制及 callback 配置模板、systemd unit、安装手册。
- 尚未部署到用户服务器；未进行真实 TikTok 授权、Ubuntu/systemd 实机或 ARM64 运行验收。
- callback TokenSink 尚未接入 `hubd` 的生产 OpenBao/凭据域；本轮不宣称整个项目生产闭环已完成。

## 最终归档

| 文件（位于 `dist/releases/`） | SHA-256 |
| --- | --- |
| `api-key-hub-0.3.0-linux-amd64.tar.gz` | `2c7bfa6d48b33428e1118ab62d8bd93c596291a6c2af3003c5abfddadd36b0b0` |
| `api-key-hub-0.3.0-linux-arm64.tar.gz` | `1dc48b90c87455a234e66844e911ea4055dabae76fa88fa32ffcab6acce74265` |

两份归档各有 18 个被内部清单覆盖的文件，另含 `SHA256SUMS`。解包逐个校验通过，ELF 架构分别为 amd64/arm64，二进制具有执行权限。callback 部署目录采用四个文件的发布白名单，不复制任意本地配置、secret、证书或私钥。

## 本轮检查与结果

使用 Docker `golang:1.27.2` 执行：

- `bash scripts/build/package.sh`：退出 0，生成两份最终归档和外部 `SHA256SUMS`。
- `go test ./...`：退出 0。
- `go vet ./...`：退出 0。
- `go test -race ./cmd/ttcallback ./internal/adapters/callback -count=1`：退出 0。
- `git diff --check`：退出 0。

另外对最终归档中的 amd64 `bin/ttcallback` 进行了实际进程验证：以 UID/GID 10001 启动，服务读取所有权正确的 0600 key/secret 文件和 0640 TLS 文件；通过受信任的本地自签证书访问 HTTPS，两个虚构 App 分别使用自己的 secret 与本地模拟 Token 接口兑换；重放 callback 返回 400 且无第二次兑换；state 文件仅含 hash，Token 文件为 0600 加密封装，响应不包含 secret 或 Token；管理路径返回 404，SIGTERM 正常退出。缺失证书时进程及时退出 1，日志仅含通用失败事件。

运行证据位于本地生成文件 `dist/releases/callback-verification.json`。模拟接口未联系 TikTok，不构成真实平台联调。

## 审查修正

| 问题 | 已采取的修正及依据 |
| --- | --- |
| 打包 callback 目录的通配符可能夹带本地敏感文件 | 改为四文件白名单，验包确认实际文件集。 |
| 安装文档 0640 key/secret 与代码要求冲突 | 改为服务账户持有 0600，非 root 进程验证读取成功。 |
| env 样例与 file 安装指引混用会同时设置两种来源 | 默认两个 App 都使用 file，文档明确 env/file 二选一。 |
| 非 root 监听 443 和远端 LB 访问回环地址说明不完整 | 文档补充 capability 覆盖配置、同机 TCP 转发、远端内网监听及 IPv4/IPv6 区别。 |
| ServeTLS 启动失败后进程仍等待信号 | 改为同时等待服务退出与信号；缺失/损坏证书回归测试先失败、修复后通过。 |

安装入口：[INSTALL.md](../../deploy/tiktok-callback/INSTALL.md)。
