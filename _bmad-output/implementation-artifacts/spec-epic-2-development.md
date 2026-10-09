---
title: 'Epic 2 Seller 授权与凭据维护开发'
type: 'feature'
created: '2026-10-09'
updated: '2026-10-09'
status: 'blocked'
baseline_revision: '51f29c7c91771553f463ababb604818e2804462c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - 'D:/code/github/third-party-api-key-hub/_bmad-output/implementation-artifacts/epic-2-context.md'
  - 'D:/code/github/third-party-api-key-hub/_bmad-output/implementation-artifacts/epic-1-local-delivery.md'
warnings: [multiple-goals, oversized]
deferred: []
---

<intent-contract>

## Intent

**已确认平台范围（2026-10-09）：** 用户明确指定 TikTok Shop 美国市场（US）、自家店铺使用的应用。按美国 Seller 自用场景核实官方协议，不自行扩大为服务其他商家的 ISV；控制台具体注册类别和权限只在对应接口需要时核实。

**Problem:** 用户要求继续完成 Epic 2；现有服务只有本地手工凭据核心，没有 Seller 授权会话、回调兑换、平台操作处置、验证或自动刷新。原规划的真实平台、结果保护和回调部署前置证据尚未完成。

**Approach:** 延续已经采用的 Go application/adapter 架构，按 Story 2.1–2.8 依赖顺序实施。先取得可引用的平台契约和保护/入口证据，再实现授权、处置及维护闭环；真实联调单独验收。平台无关本地模拟是可选缩减范围，尚不能据 Epic 1 的范围决定自动视为本轮已批准。

## Boundaries & Constraints

**Always:** 网络调用在一致性门外；领取前持久化操作；提交前复核 manage、App/resource/依赖快照、profile 与 epoch；兑换、重授权、刷新共用 App 排他协调；完整平台结果先保护再转换为当前目标/type/generation 密文；秘密不入 argv、日志、审计、失败响应或明文磁盘；保留停用决定。

**Never:** 猜测目标 Seller 协议、用 Creator/Affiliate 或通用 OAuth 替代官方适用性证据、自动扩大到公开管理/取密入口、盲重放不确定平台调用、用 fixture 代替 OpenBao/HTTPS/真实平台验收，或在未获得明确授权时更改第三方资源。

## I/O & Edge-Case Matrix

| 场景 | 输入/状态 | 预期行为 | 失败处理 |
| --- | --- | --- | --- |
| 创建会话 | 当前 manage、已登记且可授权 App | 返回受控 profile 授权链接；只持久化至少 256 bit state 的哈希、发起者、期限、epoch | 权限拒绝先于对象披露 |
| 重复/迟到回调 | 已提交操作、旧 epoch、过期或错误来源 | 已提交仅安全返回完成状态；其他不兑换、不覆盖 | 安全错误，无 query/code 回显 |
| 远端成功、本地失败 | KMS/DB/CAS/撤权故障 | protected-result 或 indeterminate，阻止 App 普通轮换 | 重启不调用平台第二次 |
| 本地条件提交 | 完整结果包及匹配快照 | 验证全部绑定并重新保护当前值，原子提交所有店铺 | 条件不匹配保留待处置状态 |
| 验证未完成 | 同 generation 暂时故障 | 记录 incomplete，保留原确定证据和期限 | 旧 generation 结果不能修改新值 |
| 自动刷新 | 共享远端资源、disabled 凭据 | 单资源领取；换值重置旧证据，保留 disabled 和既有恢复资格 | 不创建恢复资格，不重试未知副作用 |

</intent-contract>

## Code Map

- `internal/application/store.go:24` — State/normalize/clone 目前只有六类手工实体；新增会话、操作及刷新资源时必须完整深拷贝，并与事务仓储一起演进。
- `internal/application/store.go:149` — Execute 是唯一业务入口，复用权限、审计、事务发布；现有写门覆盖整个手工请求，不能直接把平台网络调用塞入 mutate。
- `internal/application/provider.go:11` — ProviderProfile 当前仅定义凭据用途和必需组合；新增能力/版本及规范化结果端口，保留未知能力拒绝。FixtureProfiles 不代表 TikTok。
- `internal/domain/domain.go` — App 已有 AuthorizationEpoch；Credential 已有 generation、停用、恢复资格和验证结果，需沿用现有版本语义。
- `internal/adapters/sqlite/repository.go:51` — schema version=1，六张 record 表；Load 使用同一读事务，Save 使用仓储 revision CAS。新增实体须同步迁移、加载和原子保存。
- `internal/application/store.go:38` — Protector 仅有当前值用途接口，不能直接表达完整待处置包绑定；需先完成明确的结果保护契约与适配器验证。
- `internal/contract/v1/contract.go:70`、`validation.go` — 23 个手工操作及严格 schema；新增操作必须同步 DTO、字段出现性校验及机器契约。
- `cmd/hub/response.go:43` — 业务响应现在限定 fixture 组合；新增真实 profile 前必须同步可信输出白名单，不透传未知秘密。
- `cmd/hubd/daemon.go:26` — 只允许 Linux fixture 启动；尚无平台 adapter、后台调度或回调监听。
- `internal/adapters/local/server.go:16` — Unix 管理入口继续保留；公开回调必须独立 mux，不复用管理路由。
- `scripts/contract-schema.py` — 同步导出请求/schema/样例及回归验证。
- `_bmad-output/planning-artifacts/contracts/secret-protection/SECRET-PROTECTION-CONTRACT.md` — 只交付本地当前值保护，明确 pending/OpenBao 未完成；这是只读前置证据，不是结果保护通过证明。

## Tasks & Acceptance

**Execution:**

1. `_bmad-output/planning-artifacts/contracts/provider/tiktok-seller-profile.md` — 完成 1.5 延期字段用途及 2.1 平台契约：核实目标市场/App 类型、权限、官方版本、授权/state/身份/shop_cipher、轮换与失败分类；依赖用户提供非敏感目标信息。
2. `_bmad-output/planning-artifacts/contracts/secret-protection/PENDING-RESULT-CONTRACT.md`、`validation-report.md`、`internal/adapters/protector/` — 补齐 1.2/1.6 结果保护设计及隔离实证，验证跨用途/operation/App/epoch/profile/快照交换、轮换及重启；不能以普通当前值测试替代。
3. `_bmad-output/planning-artifacts/contracts/linux-runtime/CALLBACK-EDGE.md`、`internal/adapters/callback/` — 2.2 独立探针与 HTTPS 精确配置验证，证明只暴露允许路由且不采集 query/code/body。
4. `internal/domain/authorization.go`、`internal/application/authorization.go`、`internal/adapters/sqlite/repository.go`、`internal/contract/v1/`、`cmd/hub/response.go` — 2.3 会话创建/检查与 2.4 durable 操作、回调归属、结果保护和多店铺原子提交。
5. `internal/application/provider_operations.go`、`internal/contract/v1/` — 2.5 非敏感操作检查及显式本地条件提交；不重放平台请求。
6. `internal/application/validation.go`、`internal/domain/domain.go` — 2.6 类型化验证尝试、同版本证据保留及迟到结果拒绝。
7. `internal/application/refresh.go`、`internal/domain/refresh.go`、`cmd/hubd/daemon.go`、`internal/adapters/sqlite/repository.go` — 2.7 共享资源持久化调度、App 协调、时钟故障停调及有界安全重试。
8. `**/*_test.go`、`scripts/contract-schema.py` — 各功能先写失败测试，再实现；外层覆盖 CLI→服务、独立回调、SQLite 重启及 daemon 调度；回归上述边界矩阵。
9. `_bmad-output/implementation-artifacts/epic-2-platform-validation.md` — 2.8 受控真实联调；仅在环境与外部动作授权齐备时执行，非敏感证据单独记录。

**Acceptance Criteria:**

- Given profile 及保护/入口门槛已通过，when CLI 创建会话、平台同意、回调及状态查询，then 正确 App/安装当前值原子更新，重复回调只兑换一次；CLI 生成 URL 不算授权完成。
- Given 网络阻塞、并发换值或撤权，when 回调/刷新完成，then 普通本地请求仍可执行，过时快照不能覆盖当前值，远端可能生效则保留 durable 待处置保护。
- Given SIGKILL 后重启及完整受保护结果，when manage 显式处置，then 只做本地条件提交，全部绑定匹配才提交，无再次平台调用且 disabled 保持。
- Given 多安装共享 refresh secret，when daemon 在 CLI 不常驻时维护，then 仅同一资源领取一次，完整更新依赖当前值；未知能力/时钟不可信时不执行真实刷新。
- Given 未授权、畸形或超限请求，when 通过 Unix 或回调最外层入口检查，then 安全失败且无任何部分秘密，审计及响应不包含输入原文。
- Given 真实受控 Seller 环境和明确外部操作授权，when 完成 2.8 矩阵，then 取得独立联调证据；未运行场景保留未验证，不能宣告完整 Epic 2 done。

## Spec Change Log

## Review Triage Log

## Design Notes

沿用已采用架构，不重新选择数据库、Go、KMS 或本地 CLI 方式。原 Epic 2 的任务均受前置证据约束；若用户选择先做本地核心，需明确调整本轮 intent/验收，所有生产/真实平台门槛继续保留，不能悄悄将完整 Epic 改为模拟交付。

## Verification

- Docker Go 1.27.2 `go test -race -coverpkg=./... ./...`、`go vet ./...`、`govulncheck ./...` — 实施后全仓回归、竞态、静态检查和 Go 漏洞检查。
- `uv run --with jsonschema scripts/contract-schema.py` — 新旧操作及样例/schema 一致。
- 回调边界、OpenBao 隔离及真实平台验证必须引用各自环境实测，不能由以上命令替代。

## Auto Run Result

- Status: blocked。Blocking condition: intent gap；目标平台输入缺失，且原完整 Epic 与缩减本地模拟范围产生不同可观察结果，当前请求未选择后者。
- Ready-for-development 自检：代码落点、依赖顺序和外层验收已记录；Sufficient/Coherent 未满足，不能将未完成平台/保护/部署前置证据修饰为已通过。
- HALT 依据：本轮渲染的 bmad-build-auto step-02 第 5 项要求存在 intent gap 时停止并记录 unanswered questions/evidence；workflow 的 Sufficient 要求没有未解决依赖或实施缺口。
- 本轮用户请求：继续完成 Epic 2。当前尚未实施新功能。
- 已完成准备：创建 `codex/epic-2` 分支，编译 Epic 2 上下文，核查代码落点和前置产物，形成上述任务与验收。
- 基线验证：2026-10-09 Docker Go 1.27.2 `go test ./...` 返回 0，全部八个包通过；仅证明现有 Epic 1 基线，不证明 Epic 2 功能存在。
- 已补齐输入：用户已明确美国市场、自家店铺使用应用，原市场/使用场景意图缺口已解除；继续调查官方适用协议、结果保护和回调前置证据。不能要求用户通过聊天提供真实 Secret/Token。
- 前置缺口：目标 Seller profile、PENDING-RESULT-CONTRACT.md、真实 OpenBao 结果包隔离及身份生命周期证据、CALLBACK-EDGE.md 与 Linux/TLS 实测均未形成。
- 版本控制：本轮只有上下文与规格文档变更，未提交、未推送、未更改第三方资源。
