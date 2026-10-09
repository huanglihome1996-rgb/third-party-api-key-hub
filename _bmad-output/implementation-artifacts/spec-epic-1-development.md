---
title: 'Epic 1 本地可验证的手工凭据核心'
type: 'feature'
created: '2026-10-09'
status: 'done'
baseline_revision: '4e10ae1f14b4544f550646b532bf918eec95b279'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - 'D:/code/github/third-party-api-key-hub/_bmad-output/implementation-artifacts/epic-1-context.md'
  - 'D:/code/github/third-party-api-key-hub/_bmad-output/planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md'
warnings:
  - multiple-goals
  - external-validation-deferred
deferred:
  - 'Ubuntu/systemd/host policy acceptance remains blocked; Docker Linux peer UID and TTY process checks passed.'
  - 'OpenBao Transit/AppRole isolation evidence remains blocked until an isolated OpenBao environment is supplied.'
  - 'TikTok Shop provider profile and live platform validation are deferred to Epic 2.'
---

<intent-contract>

## Intent

**Problem:** 仓库只有规划文档，没有可运行的第三方凭据管理核心，Epic 1 的平台无关手工闭环无法被本地验证。

**Approach:** 使用 Go 模块化单体实现平台无关的领域规则、严格 v1 JSON 契约、SQLite 持久化、CLI 和可替换的密文保护端口；本地测试使用虚构秘密和内存保护器，生产 OpenBao、Linux 权限和 TikTok profile 继续保持明确阻塞。

## Boundaries & Constraints

**Always:** 不在 argv、stdout 失败响应、stderr、日志、审计或测试数据中输出秘密；所有写操作使用 CAS/generation 和事务边界；查询必须完成归属、权限、状态和完整凭据校验后一次性输出；未知期限必须显式确认；停用不能被刷新、确认或映射变更隐式解除。

**Never:** 不实现真实 TikTok 授权/刷新、不伪造 Linux/OpenBao 验收证据、不把开发保护器当作生产后端、不开放远程取密或管理 TCP、不接受未知字段或静默 trim 标识。

</intent-contract>

## Code Map

- `go.mod` -- 固定 Go 1.27.2 与 SQLite 驱动依赖。
- `internal/contract/v1` -- 唯一的操作、请求、响应、错误码和严格 JSON 解码定义。
- `internal/domain` -- App、店铺、品牌、映射、凭据状态、generation 和恢复资格规则。
- `internal/application` -- 登记、映射、导入、确认、查询、失效、停用和恢复用例，以及端口接口。
- `internal/adapters/sqlite` -- 业务与非敏感审计表、事务仓储和迁移。
- `internal/adapters/protector` -- 可替换 SecretProtector；测试内存保护器和显式 fixture 的本地 AES-GCM 后端；后者使用独立生成的开发 key 文件，拒绝生产启动。
- `cmd/hub` -- stdin JSON CLI、完整响应验证、允许位置元数据重建、显式隐藏 TTY 和安全错误处理。
- `cmd/hubd`, `internal/adapters/local` -- fixture 本地 Unix Socket 服务；Docker Linux 进程测试不代替 Ubuntu/systemd/OpenBao 的交付验收。
- `Dockerfile`, `compose.yaml` -- 非 root、无网络、共享受保护 Unix Socket 的 fixture 服务和 CLI。

## Tasks & Acceptance

**Execution:**
- `go.mod`, `internal/contract/v1` -- 建立严格请求/响应、错误优先级、generation 和完整查询输出契约 -- 供 CLI、服务和测试共同使用。
- `internal/domain`, `internal/application` -- 实现唯一性、归属链、凭据可用性、CAS、停用/恢复和完整取密规则 -- 形成 Epic 1 的平台无关核心。
- `internal/adapters/sqlite`, `internal/adapters/protector` -- 实现 SQLite 事务仓储、非敏感审计和可替换保护端口 -- 保留生产 OpenBao 边界。
- `cmd/hub`, `cmd/hubd` -- 提供严格 stdin CLI 和本地组装入口 -- 让虚构 fixture 可端到端运行。
- `**/*_test.go` -- 覆盖冲突、缺链、未知期限、秘密不泄漏、版本冲突、停用恢复和全量失败 -- 防止核心规则回归。

**Acceptance Criteria:**
- Given 两个 App、多个外部店铺和品牌/内部店铺映射，当执行登记、映射和导入时，then 唯一性冲突整笔拒绝且旧状态不变。
- Given 当前凭据值，当使用正确目标/type/generation 查询时，then 返回 identity、credentials、status 三组 JSON；任一必需凭据不可用时整体无明文。
- Given 已知或未知期限凭据，当执行确认、mark-invalid、disable 和 restore 时，then 版本/CAS/恢复资格按架构规则生效，停用不会被隐式解除。
- Given 非法 JSON、未知字段、重复字段、缺字段或超限请求，当执行 CLI 时，then 退出码为 1，stdout 仅包含安全 error/message，秘密不出现在任何输出或审计中。
- Given 运行 Docker Go 1.27.2 构建测试，当执行格式化、单元测试和竞态测试时，then 全部通过；Linux/OpenBao/TikTok 相关验收明确记录为 deferred。

## Design Notes

实现先保证领域和应用层不依赖 SQL、HTTP、操作系统或平台；SQLite、保护器和 CLI 通过端口接入。开发保护器只用于虚构 fixture，任何生产启动路径都拒绝将其作为 OpenBao 替代。完整查询在序列化前一次性完成所有解密和状态检查，避免部分秘密输出。

## Verification

**Commands:**
- `docker run --rm -v "${PWD}:/src" -w /src golang:1.27.2 go test ./...` -- expected: all local tests pass.
- `docker run --rm -v "${PWD}:/src" -w /src golang:1.27.2 go test -race ./...` -- expected: no race reports.
- `docker run --rm -v "${PWD}:/src" -w /src golang:1.27.2 go vet ./...` -- expected: no vet findings.

## Spec Change Log

### 2026-10-09 — 实施路径澄清

- 用户选择 Docker 运行，不安装主机 Go；保留 Go 1.27.2 与既定 SQLite 版本。
- 用户明确允许本地可验证核心先实施，真实 Linux/OpenBao 验收仍阻塞；TikTok profile 延后到 Epic 2。
- 保留原 intent 的秘密边界、事务、版本与完整输出规则；fixture AES-GCM 仅作本地演示，不改变生产 OpenBao 的既定选择。

## Review Triage Log

### 2026-10-09 — Review pass

- verdicts: 28 findings — high 11, medium 16, low 0, false 1, maybe-false 0
- findings:
  - `[medium]` `[patch]` Blind 1：崩溃遗留 socket 阻止重启 — 增加排他锁及同 UID/ECONNREFUSED 检查后回收；遗留/live listener 测试及实际容器 SIGKILL 重启通过。
  - `[high]` `[patch]` Blind 2：缺 peer 身份落为 UID 0 并获权 — Handler 要求已识别 Actor；即使 authorize 全允许也返回 permission_denied。
  - `[medium]` `[patch]` Blind 3：入站拒绝缺审计 — HTTP/JSON/权限/身份失败记录 local.reject；未知身份用保留 UID 4294967295、unidentified，不冒充 root；准确审计断言通过。
  - `[medium]` `[patch]` Blind 4：失败业务审计目标丢失 — 校验后、业务执行前提取安全目标；失败过滤断言通过。
  - `[medium]` `[patch]` Blind 5：审计 mutex 等待不受 500ms 限制 — 移除该 mutex，SQL 连接等待与写入均使用带 deadline 的 context。
  - `[medium]` `[patch]` Blind 6：列表超过请求预算导致响应失败 — 最小修复将响应独立预算设为 16 MiB；同时列表支持已有 offset/limit 模式、默认100/最大1000及稳定 total。
  - `[high]` `[patch]` Blind 7：非法 Unicode 被静默替换 — 解码前检查 UTF-8 和 surrogate 配对；拒绝反例测试通过。
  - `[high]` `[patch]` Blind 8：mutation 接受缺失/负版本 — 正 row_revision 必需；凭据 mutation 正 generation 必需；恶意响应测试通过。
  - `[high]` `[patch]` Blind 9：query status 负版本/null 漏校验 — 检查原始已知字段类型与正版本，验证后重建 JSON；负 row_revision、null available 均拒绝。
  - `[medium]` `[patch]` Blind 10：metadata 嵌套字段及 deferred 元素漏校验 — 按实体逐项 allowlist，deferred 仅 string；跨实体字段和非字符串反例测试通过。
  - `[high]` `[patch]` Blind 11：query identity 不匹配显式选择 — 输出前逐个核对 App/shop/brand/internal shop；错目标断言通过。
  - `[medium]` `[patch]` Blind 12：schema/runtime 字段出现性不一致 — 首次导入禁止 expected_generation，App scope 禁止 shop 字段，空可选 ID 拒绝；操作限定 schema 与 7 项反例检查通过。
  - `[high]` `[patch]` Edge 1：小数秒期限被截断 — 使用 RFC3339Nano 保存/输出；精度测试通过。
  - `[high]` `[patch]` Edge 2：解密期间过期仍输出 — 全部解密后再检查必需凭据；时钟推进测试证明全量拒绝。
  - `[high]` `[patch]` Edge 3：canonical TTY 静默截断长值 — 关闭 ICANON，逐字节有界读取；真实交互导入并取回精确 5000 字节值。
  - `[high]` `[patch]` Edge 4：缺身份被当作 root — 同 Blind 2 的明确已识别身份守卫；拒绝和未知身份审计测试通过。
  - `[medium]` `[patch]` Edge 5：崩溃后无法重启 — 同 Blind 1；排他锁保留活动 listener，回收安全遗留 socket。
  - `[medium]` `[patch]` Edge 6：失败业务目标缺失 — 同 Blind 4；合法请求安全目标先提取，失败测试通过。
  - `[high]` `[patch]` Edge 7：SQLite Load 混读不同提交 — 元数据及六实体表置于同一读事务，保持 revision 与实体快照一致。
  - `[high]` `[patch]` Edge 8：首次 key 失败留下残缺正式文件 — 临时文件完整写入、Sync 后硬链接原子发布，已有 key 不覆盖；重启解密测试通过。
  - `[medium]` `[patch]` Edge 9：非查询 metadata 接受其他 DTO 字段 — 同 Blind 10；实体限定白名单拒绝跨类型字段。
  - `[medium]` `[patch]` Edge 10：共享 DTO 接受禁用字段空/零值 — 按操作检查字段出现性，CLI 发送前及服务权限校验后执行；空/零反例通过。
  - `[medium]` `[patch]` Verification 1：没有不同进程 UID 验证 — Linux subprocess UID 65534 经内核身份发现后被拒绝；Docker root 测试环境实际运行，未跳过。
  - `[medium]` `[patch]` Verification 2：隐藏输入只测 helper — 增加控制 PTY 的完整 interactive import、随后 query 及取消后状态不变测试。
  - `[medium]` `[patch]` Verification 3：审计过滤分页无结果断言 — 新测试准确断言时间/操作/目标/结果/操作ID筛选、稳定顺序、offset/limit 和分页前 total。
  - `[false]` `[reject]` Intent 1：相对全量 Epic 存在生产缺口 — 用户已明确批准本地核心与外部验收延期；交付及结果均不声明全量 Epic done，故不是本轮缺陷。
  - `[medium]` `[patch]` Intent 2：应用核心硬编码 fixture 规则 — 以可信 ProviderRegistry 注入替换内部硬编码，第二虚构 profile 复用测试通过；公开 fixture CLI/schema 范围保留明确说明。
  - `[medium]` `[patch]` Intent 3：auto-result 仍称没有实现及决策未答 — 已更新为用户已确认的本地核心结果和明确延期门槛，移除过时阻塞说明。

### 2026-10-09 — Focused follow-up

Edge reviewer 复核原 10 项均已解决，六包测试通过。Blind reviewer 初次复核发现未知身份审计标签残留，随后以 Linux 保留 UID/unidentified 和准确断言修复；local 竞态测试与 vet 通过。该 reviewer 再次确认原 12 项全部解决，无已知未解决的本轮缺陷。

## Auto Run Result

- **结果**：用户批准的本地核心范围 done；原完整 Epic 1 的 18 条任务和生产验收不得全部标记 done。
- **实现**：23 操作的严格 v1 JSON/CLI、可信 profile 端口、手工导入及完整组合取密、generation/CAS/停用恢复、映射及安装、SQLite 原子存储、非敏感审计、Linux fixture Unix 服务与隐藏 TTY。
- **文件**：`go.mod/go.sum` 固定 Go 1.27.2/SQLite；`internal/contract/v1` 契约校验；`internal/domain/application` 规则与用例；`internal/adapters` 持久化/保护/Unix；`cmd/hub/hubd` CLI与服务；`Dockerfile/compose.yaml` 隔离运行；`scripts/contract-schema.py` 导出验证；contracts/README/交付记录说明公开范围和延期门槛。
- **审查**：逐条 27 项修复（high 11、medium 16），1 项因已获用户范围授权且交付没有伪造全量完成而拒绝；重复根因保持独立行记录。延期项只有已确认的生产验收。followup_review_recommended=false：已有针对性复核，最后审计标签修复有准确测试，无可具体命名的未验证本轮高风险。
- **验证**：Docker Go 1.27.2 `go test -race -coverpkg=./... ./...` 全部通过，合并覆盖率 81.3%；`go vet ./...` 通过；`govulncheck ./...` 未发现漏洞。`uv run --with jsonschema scripts/contract-schema.py`：23 操作、46 样例、7 回归检查通过。
- **实际运行**：`docker compose up -d --build hubd` 成功；health revision=0、audit_degraded=false；SIGKILL 后启动和 health 成功；UID10001、目录0700、DB/key/socket0600、无 TCP 发布。daemon 测试另验证登记持久化和 SIGTERM 优雅重启。
- **依赖检查限制**：`npm audit --json` 已运行但 ENOLOCK，本项目无 npm 依赖 lockfile，不宣称 npm 审计通过；运行代码依赖以 Go 漏洞扫描验证。
- **残余门槛**：Ubuntu24.04/systemd/账户与采集实证、OpenBao Transit/AppRole/用途隔离与轮换、TikTok profile/真实平台验收继续延期；fixture 独立 key 和 per-entity JSON SQLite 不代表生产 KMS、结构性迁移及容量验收。
- **版本控制**：在已获授权分支提交审查后的本地交付；不推送。
