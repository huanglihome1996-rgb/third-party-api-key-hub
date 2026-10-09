---
title: 修复 Epic 3 审查中的启动与容量风险
type: bugfix
created: 2026-10-09
updated: 2026-10-10
status: done
route: dispatch
baseline_commit: b184fc1c30d1ebf12e05cb9c9b253db574636655
review_loop_iteration: 1
context: []
---

<frozen-after-approval reason="用户已明确要求把发现的问题都解决掉">

## Intent

**Problem:** 已有业务/审计文件可被初始化逻辑静默重建；审查中的历史扫描容量风险缺少可复跑证据。

**Approach:** 区分新建与已有数据库，验证已有库后才允许迁移或服务启动；用规模明确的历史负载验证调度、保留和查询，并处理被实测证实的性能问题。

## Boundaries & Constraints

**Always:** 已有空库、损坏库、缺表/元数据和异常约束安全拒绝；合法旧 schema 迁移与 SQLite 崩溃恢复仍可用；业务 revision、恢复关闭及 audit_gap 保留。缺失审计库沿用持久 gap 策略。检查完整性不使用 immutable 忽略活动日志；不接触现有实例或真实秘密。

**Never:** 不跳过完整性或权限验证、不缓存跨请求明文、不自动裁剪备份/审计改变保留规则、不把 fixture 容量结果外推成真实生产验收。OpenBao/Seller/目标主机未提供时仍保持未验收。

## I/O & Edge-Case Matrix

| 场景 | 输入/状态 | 结果 | 失败处理 |
|---|---|---|---|
| 新实例 | 数据库路径从未存在 | 初始化后正常启动 | 初始化失败不提供 IPC |
| 业务库损坏 | 空、截断、缺 metadata/表、额外 schema | 拒绝启动 | 不补齐缺失数据 |
| 审计库损坏 | 空、异常约束/trigger/index/记录 | 拒绝启动 | 不生成健康空历史 |
| 审计库缺失 | 已有合法业务库 | gap 持久化后新建审计库 | gap 写失败拒绝启动 |
| 旧版/崩溃 | 完整 v1、合法 rollback/WAL | 恢复已提交数据并迁移 | 不忽略日志，不丢 revision |
| 历史负载 | 明确事件数、包数和规模 | 全量验证及查询实测 | 超预算保留失败样本并修复 |

</frozen-after-approval>

## Code Map

- `internal/adapters/sqlite/repository.go`：New/NewAudit 建表发生在校验前；open 设置连接 PRAGMA；完整旧 schema 必须迁移。
- `internal/adapters/sqlite/operations.go`：Inspect 当前 immutable 专用于快照；提取连接级结构验证供启动复用，兼容 v1。
- `cmd/hubd/daemon.go`：业务 existing 判定、密钥验证、validateAdmission 缺失审计 gap、IPC 启动。
- `internal/adapters/backup/backup.go`：Schedule 对所有历史包完整验证。
- `internal/application/read.go`：audit.list 全量加载/筛选/排序；当前容量风险的测量路径。

## Tasks & Acceptance

**Execution:**
- [x] SQLite 及 daemon 回归测试先证明 P1：空/截断/缺 schema 拒绝且不会被补齐，合法 schema 与历史保持。
- [x] 修复 SQLite 构造器的新建/验证边界，复用 schema/integrity/record 校验并支持完整 v1。
- [x] 验证 daemon 未创建 IPC、保留缺失审计 gap、崩溃恢复和恢复关闭语义。
- [x] 规模明确的历史容量测量、针对真实瓶颈修复与故障保留检查。
- [x] 更新审查记录、运维手册和修复验证证据；独立审查实现。

**Acceptance Criteria:**
- Given 已有损坏业务或审计库，when 重启，then 在提供 IPC 前失败，文件不被静默初始化。
- Given 完整 v1 或合法崩溃日志，when 重启，then 合法迁移/恢复保留提交状态与非敏感历史。
- Given 明确 fixture 历史负载，when 执行完整 CLI/维护，then 记录容量、全部结果、预算与实际时延，不隐藏超时。

## Implementation Notes

本轮用户授权实现全部审查修复；新增审查文件是前轮已交付产物，保留它。没有生产环境/第三方材料的验收项是边界事实而非代码缺陷。

执行分工：实现代理负责 SQLite 构造器/Inspect 复用及 daemon admission 与回归测试（internal/adapters/sqlite、cmd/hubd）。容量代理负责 scripts/operations/capacity 与测量，以及确有证据的 backup Schedule 改进。主代理负责规格、文档、集成验证与复核；各代理保留其他人的修改。

## Spec Change Log

- 2026-10-09：独立审查确认完整 v1 的 JSON 可解码并不证明业务语义及密钥绑定合法；启动 admission 必须在迁移和 durability 切换前运行业务校验。保留已实现的新建边界、固定 schema 校验、v1 正常迁移、真实 WAL/热 journal 测试，重新派发该根因修复；不扩大 CLI 契约。
- 2026-10-09：容量工具将完整 Schedule 证据与 CLI 并发证据分别交付，不用 health 就绪或短时 CLI 结束冒充自动扫描完成。CLI 报告须逐项证明 backup 与 retention 的并发覆盖，超时与公布预算一致，失败保留覆盖实际执行路径。
- 2026-10-09：固定源码、两CPU配额的CLI容量报告真实FAIL（backup p95 2,135.248ms、retention 2,000.863ms，200请求成功），不使用旧二进制的伪PASS。CPU采样确认 strict JSON 解码/分配为主要成本；检查审计历史改为逐行验证，strict decoder仅复用不可变Go类型字段元数据。记录/明文/校验结果均不缓存，原逐条语义与Unicode/重复字段/大小写/nullable检查不变；必须以新冻结源码重测。

## Review Triage Log

| ID / 来源 | 判定 | 证据与处置 |
|---|---|---|
| B1 blind：语义错误 v1 在拒绝前迁移 | medium / bad_spec | New 在 daemon 的 ValidateRestoreState 前调用 migrateLegacy；`apps={}` 语法合法却无合法身份。重新派发 admission 在迁移前验证语义和密钥，并断言拒绝不改 schema/bytes。 |
| B2 blind：不完整审计记录被接纳 | medium / patch | List 仅 DecodeStrict，`{}` 的零时间会到 retention 才报错。共享持久事件校验及启动回归。 |
| B3 blind：恢复 sidecar 未检查 | medium / patch | 正常连接读取 WAL/journal，主文件检查不覆盖 sidecar 链接和权限。补充打开前 sidecar 检查与合法恢复回归。 |
| B4 blind：20 秒与 120 秒预算不一致 | medium / patch | common.request 固定 timeout=20，capacity 的维护预算为 120000ms。给维护调用一致的超时并保留明确错误及耗时。 |
| B5 blind：仅 backup 重叠也 PASS | medium / patch | summarize 的 any-overlap 可由 backup 单独满足。每种维护分别运行查询批次并分别检查重叠。 |
| B6 blind：CLI 未等待完整 Schedule | medium / patch | daemon 在 health 后异步开始扫描，CLI 的退出不证明扫描完成。最终证据必须配对完整 Go Schedule 测量与 CLI 报告，文档区分两者。 |
| B7 blind：合成数据准备与自动 retention 竞争 | medium / patch | 自动维护可能先删除旧合成历史，再做手工源备份。建立确定的历史准备边界且核验实际事件数，不能用跳过检查制造 PASS。 |
| B8 blind：晚期异常丢失已完成样本 | medium / patch | list(map) 和 job.result 后才写入报告；未捕获的后续异常会丢弃先前结果。逐个追加，实际执行路径测试晚期失败。 |
| B9 blind：损坏 recovery 文件缺乏测试 | medium / patch | 实际 RED 证明损坏 WAL header/已提交页、提交截断和损坏 hot journal 可导致旧/未提交状态被接受。已加入打开前格式/校验和检查和真正崩溃回归；原生稀疏 checksum 不能识别全部位损坏，明确保留能力边界。 |
| E1 edge：非法 v1 先迁移 | medium / bad_spec | 与 B1 同根因，但单独保留本项判定；apps 嵌入身份不合法在迁移后才被 daemon 拒绝。 |
| E2 edge：最大 revision 溢出 | low / patch | persisted MaxInt64 通过非负检查，下一次 revision++ 会为负。直接拒绝已耗尽计数并加入边界回归。 |
| E3 edge：审计序列耗尽 | low / patch | sequence=MaxInt64 被接受，AUTOINCREMENT 后续插入 SQLITE_FULL。直接拒绝耗尽序列并测试。 |
| E4 edge：cgroup 读取失败无报告 | low / patch | read_text 在 try 之外，OSError 绕过报告。移到失败报告保护内。 |
| E5 edge：维护 timeout 隐藏完成延迟 | medium / patch | 与 B4 同根因，TimeoutExpired 合并为通用错误且预算不一致。维护超时需有固定类别及实际等待时间。 |
| E6 edge：准备失败无结构化 Go 报告 | medium / patch | report defer 注册晚于 fixture/INSERT，准备阶段失败没有 JSON。报告生命周期提前，保留失败阶段。 |
| V1 verification：非法 v1 的拒绝无回归 | medium / patch | 既有拒绝矩阵仅 v2；合法 v1 测试无法阻止迁移先于验证的回退。新增非法 v1 断言 schema/version/bytes 不变。 |
| V2 verification：预算门没有独立失败测试 | medium / patch | 现有 slow 样本自带 error；删除 p95/维护预算门仍会 FAIL。加入成功样本超预算、每种错误/重叠缺失和有效 PASS 的独立用例。 |
| R1 recovery复核：WAL盐损坏伪装旧代 | medium / patch | 实际两个salt位损坏让revision8退回1；先检查当前rolling checksum，拒绝当前帧盐损坏，同时允许有已验证前缀的真实旧代尾部。首帧及提交后盐损坏回归通过。 |
| R2 recovery复核：原生journal遗漏未采样损坏 | medium / 能力边界 | 实际单个未采样字节让operation_seq11变10；原生稀疏checksum不覆盖全页。明确手册及验收声明，不伪造全状态认证；全页防篡改需要独立可信证据，当前格式没有该能力。 |
| R3 recovery复核：RESTART pending前缀被拒绝 | medium / patch | 真正崩溃和独立SQLite副本证明应恢复revision1/seq11/apps100；把旧代尾部前缀判定从“已提交”修正为“已验证”，盐损坏保护保留。 |
| R4 recovery复核：同代回滚尾部被拒绝 | medium / patch | 回滚大事务后提交较短事务会保留同salt旧pending尾部；实际副本恢复revision8/seq12/apps0。允许该pending尾部但检查后续同代提交帧；破坏完整提交链仍拒绝。最终提交标记被抹掉的格式不可判别边界明确说明。 |
| C1实际CLI容量：p95超过2秒 | medium / patch | 固定源码与两CPU配额实测backup2135.248ms/retention2000.863ms；保留FAIL，按CPU采样改为审计逐行检查和不可变字段类型元数据共享，原严格校验全部保留，最终同规模重测PASS：backup/retention查询p95=1935.176/1978.388ms，200/200成功；完整扫描与保守维护83.719秒，存储环境差异明确记录。 |

## Verification

最终命令、实际结果和环境见 [修复验证记录](review-remediation-validation.md)。专项回归先失败再通过；最终全量 race/coverage、vet、漏洞扫描、静态契约、Python 和隔离恢复演练分别取证。容量证据明确环境/规模与复跑命令；不把原始基线检查当作当前代码的通过证据。

## 修复处置与复核

- B1、E1、V1：`NewAdmitted` 在完整 v1 迁移前执行业务语义与独立 key 校验；非法 legacy 测试断言 schema、version、bytes 不变。
- B2：共享审计事件 decoder；启动、list 与 retention 均拒绝缺字段或非法持久事件。
- B3：主文件及 WAL/SHM/journal 的类型、权限与孤立 sidecar 在打开前检查；daemon 回归确认 IPC 不开放。
- B4、E5：经实际 CLI/IPC 契约复核，手工期限应保持15秒，supervisor20秒；自动完整扫描独立使用两分钟预算。修正报告口径和固定 `cli_timeout` 分类，没有扩大生产 deadline。
- B5：手工 backup/retention 各自拥有查询批次、重叠检查和 p95 门槛。
- B6：Go 完整 Schedule 报告与 CLI 报告分别证明自动扫描与手工并发；CLI 明确 `automatic_maintenance_verified:false`。
- B7：CLI 合成历史全部在364天内，核验事件数并保留每包全部事件；Go 单独测量真实过期事件删除。
- B8：逐个保存完成样本；晚期 worker 异常和执行路径故障回归保留先前证据。
- B9：完整 committed WAL/hot journal 损坏和合法重用均有真实崩溃回归；补充盐损坏拒绝，稀疏 rollback 校验和边界已写入手册。
- E2、E3：业务计数和审计 sequence 的 MaxInt64 耗尽直接拒绝并回归。
- E4：环境读取在结构化失败报告保护内，实际 OSError 测试覆盖。
- E6：Go 报告生命周期提前；TMPDIR=/proc 的实际失败报告保留 `fixture_creation/setup_failed`、FAIL 和 `full_validation:false`。
- V2：有效成功样本单独超查询/维护预算、缺 retention 重叠以及维护错误分别导致 FAIL，有效对照 PASS。

独立恢复复核补充发现：WAL 两个 salt 位损坏可伪装旧代尾部，已修正为先计算当前 checksum 再判断旧代；合法RESTART后pending当前代前缀、回滚后较短事务覆盖的同代pending尾部均已修复并独立实际复核。原生SQLite rollback的未采样字节损坏、WAL最后提交标记损坏为零与pending尾部不可区分是格式限制，采用明确能力说明，不宣称全状态认证或任意损坏检测。

## 最终完成依据

2026-10-10：冻结源码全量race、合并覆盖率81.7%、vet、govulncheck、静态契约27/54/10、Python12测试、最终归档14项恢复演练均通过。原17项逐项处置和新增恢复/性能发现已完成复核；独立检查无残留具体发现。容量失败样本及构建来源保留，最终CLI与完整Go扫描均在明确实测范围通过。SQLite格式能力边界及真实生产门槛保留为未证明，不以fixture关闭。
