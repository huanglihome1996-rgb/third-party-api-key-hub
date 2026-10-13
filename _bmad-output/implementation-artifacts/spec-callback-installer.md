---
title: 'ALB 后的 Callback 一次安装脚本'
type: 'feature'
created: '2026-10-13'
status: 'complete'
baseline_commit: '8d86893400651b10e638b11b42f031424b18ad88'
route: 'dispatch'
review_loop_iteration: 0
context:
  - 'deploy/tiktok-callback/config.example.json'
  - 'deploy/tiktok-callback/api-key-hub-ttcallback.service'
  - 'internal/adapters/callback/config.go'
  - 'cmd/ttcallback/main.go'
---

## Intent

用户提供 app_id、app_key、app_secret、公网 callback 域名，一条安装命令自动配置并启动 callback。最新指令明确由外部 ALB 处理 HTTPS 和证书，本服务只提供 HTTP 后端，由用户配置转发。

## 设计和约束

- Ubuntu 24.04/systemd、amd64/arm64，在完整安装包内执行 `sudo bash deploy/tiktok-callback/install.sh`。Bash 按需安装 Python3，Python 标准库负责交互与安装。
- 默认监听 `0.0.0.0:9080` HTTP，可用 `--listen-addr` 指定实际 IPv4 地址与非特权端口。ALB 的公网 HTTPS 域名仅用于生成 `redirect_url=https://域名/callbacks/tiktok-shop`；拒绝 IP、路径、查询、端口、HTTP 公网域名、通配符与注入字符。
- 不配置证书、Certbot、续期或 ALB，不要求本机80/443，不修改 DNS、安全组或防火墙。用户负责公网域名、ALB HTTPS listener、HTTP target group和服务器入站规则。安装成功输出目标HTTP端口、健康检查路径和公网回调地址。
- `ttcallback -http` 明确启用 HTTP；默认 TLS 模式保留，与证书参数同时使用 `-http` 时拒绝启动。公开路由、state和Token加密协议保持不变。
- App secret 只通过 getpass 隐藏输入，不接受命令参数或环境变量，不输出秘密。每个 App 创建独立0600文件，可继续录入多个 App。App id用于配置索引，不等同于 Seller service_id；可选 `--service-id` 提供本次 App 的 US Seller 授权参数，无此值仍启动 callback并提示授权尚需补充。
- 标准 config、secret、key、Token/state 路径不变。更新按App id进行，保留其他App和现有数据；已有非标准路径或自定义unit/drop-in拒绝接管，支持旧官方TLS unit/已知capabilities drop-in迁移到HTTP。旧Token存在但key缺失必须中止，不能生成新key。
- 安装二进制、服务账户、目录、systemd配置、开机启动，检查systemd active与本地HTTP `/healthz`。失败恢复原配置、secret、binary、unit及原运行状态；不回写实时Token/state，不丢失有数据对应的key。
- 显式设置程序目录0755，不依赖调用者umask；补齐Python3/iproute2，核对服务用户与同名组。恢复systemd的持久或运行期启用状态。回滚须先确认进程停止；有实时数据变化时保存root专用恢复快照，新增key对应已有Token时保留key，不以配置是否首次创建决定key生存期。
- 已有自定义授权/回调路径拒绝自动接管。合法的空/null authorize_params按空map处理；健康探测必须拒绝非200响应和连接失败。
- 域名更新同步所有App的redirect_url并提示更新TikTok后台。配置仅含录入App，不保留第二个示例占位App。

## Code Map

- `deploy/tiktok-callback/install.sh`、`install.py`：一次安装和HTTP健康检查。
- `scripts/operations/test_callback_install.py`：真实临时文件，模拟apt/systemd边界，覆盖多App、回滚和数据保留。
- `cmd/ttcallback/main.go`、`main_test.go`：显式HTTP模式及真实子进程回归，保留TLS兼容。
- `scripts/build/package.sh`、`prepare-operations-image.sh`：版本0.3.1，白名单加入安装脚本、可执行权限。
- `deploy/tiktok-callback/INSTALL.md`、`README.md`、根README、`docs/operations.md`：首选ALB转发+HTTP后端的一次安装；手工TLS部署保留高级说明。
- `.gitattributes`：固定Linux脚本/相关源文件为LF；不做全仓无关renormalize。

## Tasks & Acceptance

- [x] Given 四项输入，when 执行安装器，then生成单App secret/config/key及HTTP 9080 unit，完成启动与健康检查流程；文件真实，Ubuntu/systemd命令在回归中模拟，实机安装未执行。
- [x] Given 外部HTTPS域名和HTTP后端，when在本地发起授权和回调，then公开redirect_url仍为HTTPS，多App/state/Token加密协议不变；真实ALB联调未执行。
- [x] Given 多App/重跑，when更新或新增App，then其他App、key、Token/state保留。
- [x] Given 非法输入/端口占用/启动失败，when执行安装，then错误中不含secret，恢复已有文件和服务状态，不回写Token/state；实时数据与未停止进程适用保留规则。
- [x] Given默认TLS或显式HTTP，when启动进程，then分别使用TLS/明文HTTP，冲突参数和缺失TLS参数返回非零。
- [x] 文档与双架构0.3.1安装包完成，不覆盖v0.3.0。

## Verification

- Linux Docker：安装器Python回归、Bash/Python语法，Go test/vet、govulncheck。
- 包内内部SHA、ELF、LF/执行权限、安装入口帮助和包内安装器回归。
- amd64实际运行HTTP和HTTPS子进程，使用合成App凭据和本地Token上游验证协议，不使用真实secret。
- 尚未在用户Ubuntu/systemd/ALB环境安装，也不代用户申请证书或进行真实TikTok授权；ARM64仅构建和结构校验。

### 已执行验证

- 2026-10-13：根代理复跑安装器28项回归全部通过，覆盖权限、运行数据保护、多App、迁移与失败恢复；两个独立审查代理复核后无剩余阻塞项。
- Go全量 `go test ./...`、`go vet ./...` 通过；`govulncheck ./...` 未发现漏洞。HTTP/TLS子进程与 `go test -race ./cmd/ttcallback -count=1` 通过。
- 0.3.1最终包从实现提交 `6f13b8c` 的 `git archive` 构建，双架构的20个包内文件SHA、ELF架构、LF/执行权限、安装帮助和各28项安装器回归全部通过。
- amd64发布二进制以非root运行HTTP（无证书文件）和HTTPS，验证双App、本地模拟上游Token兑换、HTTPS公开redirect、state哈希/重放拒绝、加密Token与SIGTERM退出。验证范围记录于Release附件 `callback-verification.json`；不构成真实ALB、TikTok或Ubuntu/systemd验收。

## Spec Change Log

- 2026-10-13：用户明确外部ALB负责HTTPS，删除安装器ACME/TLS自动化方案，改HTTP后端。保留隐藏secret、多App、幂等数据保护、事务回滚和原TLS运行兼容。
- 2026-10-13：根据并行审查补充受限umask、命名组、运行期启用状态、真实HTTP失败响应和升级期间实时数据恢复约束；保留已通过的HTTP入口、合成凭据流程、多App与原TLS兼容，不改变公开HTTP协议。

## Review Triage Log

| ID | 判定 | 证据与处理 |
| --- | --- | --- |
| B1 | medium | mkdir隐式父目录受umask影响，0700使非root服务无法执行；显式管理0755程序目录并补非root检查。 |
| B2 | high | 旧配置存在时实时Token/state未保护新App凭据，且新key可能被删除；保留对应key并保存失败安装的私密恢复快照。 |
| B3 | medium | existing_config未拒绝自定义HTTP路径而install强制改默认，可能破坏既有转发；预检拒绝。 |
| B4 | medium | enabled-runtime被布尔压缩成disabled；记录并恢复运行期启用状态。 |
| B5 | medium | 已有用户不代表unit指定的同名组存在；安装前创建/解析同名组。 |
| B6 | medium | ss缺失时运行中重跑误报自己的端口；Bash补齐iproute2。 |
| B7 | low | 自定义--binary可指定不支持-http的旧程序，但帮助明确要求支持-http，真实重启失败会回滚；完整新版包已绑定构建和SHA，不额外扩展二进制身份接口。 |
| B8 | medium/defer | 原有Token sink直到写入才读取密文，错误32字节key/损坏密文不能由健康检查判别；这是baseline已有行为，记录后续只读存储诊断需求，本轮不宣称存量数据解密验收。 |
| B9 | low | 简化手册删除了SHA验证示例；补充归档及包内校验命令。 |
| B10 | medium | FakeSystem用测试UID，不能发现非root目录遍历问题；补受限umask下真实非root访问回归，目标systemd实机验收仍明确未执行。 |
| E1 | medium | Go接受null参数map，Python合并与成功输出会崩溃；规范化空map并回归。 |
| E2 | high | 忽略stop失败后恢复文件可能删除仍运行进程的key；确认停止后才恢复，未停则保留文件并报回滚未完成。 |
| E3 | medium | 与B4相同的运行期启用状态丢失；共同修复。 |
| E4 | high | 与B2相同的新增App实时数据关联丢失；共同修复并补升级失败回归。 |
| V1 | medium | 真实health测试只有200，模拟失败不能约束实际status判断；补真实404/500/连接失败测试。 |
| V2 | high | 已有config、无key/Token时生成新key，失败后CONFIG非首次导致删key；共同修复B2并单独回归该组合。 |
