---
title: 'Epic 3 平台无关运行与恢复核心'
type: 'feature'
created: '2026-10-09'
updated: '2026-10-09'
status: 'done'
baseline_revision: '51f29c7c91771553f463ababb604818e2804462c'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
warnings: [multiple-goals, oversized]
deferred:
  - summary: >-
      大历史审计量下的事务保留扫描与全局门等待尚未测量。
    evidence: |-
      实现遍历审计事件并逐条删除；当前真实 CLI fixture 测量通过，但尚无生产历史量证据证明15秒deadline或2秒目标会失败。需代表性一年事件量、存储和并发测量决定是否增加时间索引。
    location: >-
      internal/adapters/sqlite/operations.go:89
    severity: medium (unverified)
  - summary: >-
      大备份历史量下每日全包验证的预算尚未测量。
    evidence: |-
      Schedule完整验证历史包，当前fixture正常；没有给定/复现包数量或容量导致两分钟预算耗尽。需按目标磁盘/数据量测量，生产容量与保留策略仍未验收。
    location: >-
      internal/adapters/backup/backup.go:72
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** 手工核心已有持久密文、独立审计和 Unix CLI，但缺可复现归档、审计保留、一致备份、安全恢复及运行演练工具。

**Approach:** 用户于 2026-10-09 明确选择“先完成 Epic 3 平台无关代码与 Docker 验证，生产和真实平台验收保留未完成”。沿用 Go 1.27.2、SQLite 和现有 fixture 核心，交付服务统一运维入口及可复跑工具；只对本轮缩减范围标记完成，不将原 Story 的生产门槛伪报通过。

## Boundaries & Constraints

**Always:** manage 才能备份、清理或开放恢复；权限优先、提交前复核；两库快照冻结并排空包括边界拒绝在内的审计；SQLite 自身生成快照，不复制活动 DB；完整 manifest 校验、固定 schema/模式/配置/构建绑定、0600/0700、同文件系统原子发布，失败保留既存备份。开发密钥单独保护，包只含指纹/版本引用。恢复到新目标先校验完整包与独立现有密钥，持久关闭取密，复核绑定当前 revision，显式开放，重启保留关闭；保留各凭据停用/版本证据。至少一年 UTC 审计，恰好边界保留，损坏/异常时钟不清理。单实例锁在存储初始化/恢复前取得。安全告警只含固定枚举和数字，不输出路径、底层错误、payload、秘密或密文。

**Never:** 写入生产配置、真实平台调用、对外推送/部署；打包 key、业务数据或真实秘密；生成缺失恢复 key；覆盖正在运行的实例/现有恢复目录；从审计补造恢复资格；自动解除停用；明文缓存/管理 TCP。Docker fixture、交叉编译、自动计时不替代 Ubuntu/systemd、双架构目标运行、OpenBao、Seller 联调或人工 RTO 验收。

## I/O & Edge-Case Matrix

| 场景 | 输入/状态 | 预期 | 失败处理 |
| --- | --- | --- | --- |
| 在线备份 | 并发变更/取密/边界拒绝 | 排空后两库一致快照，manifest 含业务 revision、audit gap、时间和文件哈希 | 未完成目录不发布，旧包不改 |
| 保留 | cutoff 前、等于、之后/闰年事件 | 只清理超过至少一年的事件，边界保留 | 损坏记录/未来时间/时钟倒退拒绝清理 |
| 恢复 | 完整包+独立匹配 key+新目录 | integrity/schema/hash/binding 验证后发布关闭状态 | 缺 key、错 key、损坏、路径逃逸/符号链接、已有目标均安全拒绝 |
| 恢复开放 | manage、显式账户/归属/值核对、expected_revision | 复核全部现有逻辑凭据绑定及完整安装业务组合；允许保留停用/过期目标继续不可用；原子持久开放 | 过时 revision、撤权、缺链/缺必需项/错误绑定继续关闭 |
| 重启 | 恢复后未开放/第二实例 | 跨重启仍关闭，第二实例不触碰数据库或 key | 取密整体失败，无部分明文 |
| 演练 | 10 并发完整 CLI、已校验快照故障恢复 | 只记录延迟、错误码、故障/快照/开放时间，不落响应秘密 | 失败及慢样本保留，fixture 指标标明非生产证据 |

</intent-contract>

## Code Map

- `internal/application/store.go:149` — 唯一业务入口，现有领域锁先于 deferred audit 释放；新增外层运维门必须覆盖完整审计。
- `internal/application/boundary.go:11` — 拒绝审计同样纳入排空。
- `internal/application/read.go:161` — 完整归属、组合、状态、绑定解密可复用恢复复核；健康结果同步新增状态。
- `internal/adapters/sqlite/repository.go:118` — revision CAS/全状态事务；两库独立连接。增加安全快照、保留和持久恢复字段迁移，仅迁移实际字段。
- `internal/adapters/protector/fixture.go:20` — key 缺失自动生成只可用于新 fixture，恢复必须使用已有材料。
- `internal/adapters/local/listen_linux.go:26` — 已有 flock；前移锁边界到所有数据库/key 初始化之前。
- `cmd/hubd/daemon.go:67` — Shutdown 尚未排空就可能 Close 数据库，必须等待请求结束。
- `internal/contract/v1/contract.go:70`、`validation.go:77`、`cmd/hub/response.go:125` — 操作清单、严格输入和安全非秘密输出必须共同更新。
- `scripts/contract-schema.py:37` — 操作表/请求样例/schema 需同步，不能只改 Go。
- 原项目工作区的 Epic 2 blocked spec 为前置状态证据；本轮不改写它、不以 Epic 1 核心代替 Epic 2。

## Tasks & Acceptance

**Execution:**

1. `_bmad-output/planning-artifacts/contracts/operations/OPERATIONS-CONTRACT.md` — 固化一年时间语义、冻结边界、manifest、独立 key、恢复状态机、10 并发/RPO/RTO 口径及 fixture/生产区别。
2. `internal/application/operations.go`、`store.go`、`boundary.go`、`read.go`、`internal/contract/v1/`、`cmd/hub/response.go` — 增加 manage 运维操作 `operations.backup`、`operations.retention`、`recovery.status`、`recovery.open`；服务端固定目录，客户端不得提供任意路径；持久关闭/复核/开放、外层运维门和严格契约。
3. `internal/adapters/sqlite/operations.go`、`repository.go`、`internal/adapters/backup/` — 实际 schema 迁移、VACUUM INTO/等价 SQLite 快照、事务保留、固定允许文件 manifest、哈希/integrity/绑定验证、原子快照和离线恢复；运行包不包含 key。
4. `cmd/hubd/daemon.go`、`internal/adapters/local/` — 初始化前锁、离线 `--restore-from` + `--fixture-key-file`、固定备份根目录、每日非敏感任务、白名单告警和正确 Shutdown 排空；无调度能力时明确不启用平台调度。
5. `**/*_test.go`、`scripts/contract-schema.py`、CLI-CONTRACT — 先失败测试再实现，覆盖上表、SQLite迁移/重启/失败、CLI 最外层和 fixture daemon；同步新增机器契约。
6. `scripts/build/package.sh`、`deploy/systemd/`、`docs/operations.md` — 锁定工具链双架构 hub/hubd 归档、固定 metadata/时间/排序的可复现 tar.gz、构建信息/哈希清单；unit 明确仅 fixture，不冒充生产部署。两次重复构建比较，两架构执行当前 CLI/拒绝启动检查，区分模拟 arm64 与实机。
7. `scripts/operations/benchmark.py`、`drill.py` — 完整 CLI 10 并发 nearest-rank p95、成功/失败/全部样本；启动已知虚构值 isolated 实例，复核响应只在内存；完整备份到关闭恢复再显式开放的故障演练，输出 fixture RPO/自动耗时而非人工生产 RTO。
8. `_bmad-output/implementation-artifacts/epic-3-validation.md`、README — 逐 Story 本轮产物、已验证证据与剩余门槛；保留所有不满足项。

**Acceptance Criteria:**

- Given 当前 Go 核心与 Docker 可用，when 重复构建双架构并运行验证，then 相同输入归档字节一致，归档无 key/DB/秘密，缺生产配置仍拒绝启动，实际环境记录清楚。
- Given 历史事件与并发请求，when CLI 执行保留/备份及故障注入，then 一年内及边界事件保留、两库快照可验证且含既有审计缺口，失败不损坏既存可用包，权限错误先于对象信息。
- Given 匹配独立 key 和快照，when CLI/daemon 离线恢复到新目录并重启，then 取密持续关闭，manage 明确复核当前 revision 后才开放，停用/过期/缺链/越权继续无任何部分明文。
- Given 隔离虚构组合，when 执行10并发benchmark和备份恢复演练，then 输出真实样本/错误/百分位和可核验时间，全部报告标明 fixture，不宣称生产 KMS 或人工 RTO 达标。

## Spec Change Log

## Review Triage Log

### 2026-10-09 — Review pass

- verdicts: 18 findings — high 0, medium 12, low 3, false 1, maybe-false 2
- findings:
  - `[medium]` `[patch]` Blind 1：现有错误key仍启动/备份 — Docker已复现，启动及发布前验证当前密文与独立材料的实际绑定；与Edge 1/2共享缺少绑定校验根因。
  - `[medium]` `[patch]` Blind 2：缺审计库被静默重建 — 现有business且audit缺失路径可达，须在服务健康前持久记录gap；与Edge 3共享根因。
  - `[medium]` `[patch]` Blind 3：恢复结构缺少唯一归属/provider ID及正revision验证 — normal管理已有这些约束，修改备份并更新hash后恢复开放可复现；补足现有验证器。
  - `[medium]` `[patch]` Blind 4：schema忽略PK/nullability — columns已读取却未比对，缺约束的DB可被接纳且重复ID可破坏保留/加载；补足实际schema约束校验。
  - `[medium]` `[reject]` Blind 5：24h触发含快照耗时可能越过生产RPO — 完整生产连续RPO已被用户明确排除；本轮只证明持久每日计划和fixture故障时的实测RPO，未声称连续生产≤24h。
  - `[low]` `[reject]` Blind 6：超大凭据集合超过1MiB无法开放 — 该上限是真实协议限制，当前隔离手工fixture规模未触及；增加批量复核公共状态机并非直接修正，极少在本轮fixture日常使用遇到。残余容量限制明确记录，不能扩称生产容量通过。
  - `[medium]` `[patch]` Blind 7：默认100条status页导致复核不完整 — 默认pageBounds明确为100，手册原未要求遍历；工具遍历所有页并绑定一致revision，手册说明分页。
  - `[maybe-false]` `[defer]` Blind 8：一年事件扫描使取密超时 — 实现确实全扫描，但没有证明本轮数据规模/存储会超时；需代表性历史量测量，记录为medium未验证。
  - `[maybe-false]` `[defer]` Blind 9：历史备份全验证耗尽两分钟 — 路径确实存在，尚无包数量/容量或超时复现；需容量/历史量证据，记录为medium未验证。
  - `[low]` `[patch]` Blind 10：快照/遗留staging占满共盘 — 当前没有自动裁剪策略，补直接运维指导：停服务后清理弃用包/staging，先验证保留恢复点和独立key；不引入未经确定的自动删除策略。
  - `[medium]` `[patch]` Blind 11：manual运维失败健康/告警不变 — operate错误路径只返回，maintain独占降级；依赖失败设置operations_degraded并由daemon固定枚举报警，权限/输入拒绝不伪报存储故障。
  - `[low]` `[reject]` Blind 12：门锁等待不响应context — 阻塞等待确实存在，但入门后检查取消且两分钟maintenance/停机取消控制实际操作；本轮fixture常规维护短时。新增可取消RW admission机制复杂度超出直接修正，残余等待限制记录，不承诺生产大负载deadline。
  - `[medium]` `[patch]` Verification 1：一年最短保留测试循环依赖cutoff — 已验证测试证据规则成立；增加独立日期期望、时区和一年内历史事件。
  - `[medium]` `[patch]` Verification 2：未比较恢复审计历史 — 已验证测试证据规则成立；真实SQLite快照/恢复读回并比较时间/operation ID/result/target。
  - `[false]` `[reject]` Intent：广义平台无关产品完成与fixture完成的读法差异 — 用户明确在Epic2和生产未实现的澄清问题中选择本地/Docker范围；diff及报告只称fixture本轮完成，不声称完整生产产品。
  - `[medium]` `[patch]` Edge 1：错误现有key启动 — 与Blind 1同一复现和绑定校验根因，启动前拒绝。
  - `[medium]` `[patch]` Edge 2：运行中key文件替换产生不可恢复包 — 与Blind 1同一绑定校验根因，实际独立key解密校验后才能发布。
  - `[medium]` `[patch]` Edge 3：既有审计缺口没有保留 — 与Blind 2同一缺失数据库复现，初始化前记录持久gap。

## Design Notes

沿用架构已确认的冻结、独立密钥、默认关闭和显式开放，不重新选择技术栈。新增可执行脚本只消费非敏感结果。恢复开放不对所有停用或过期凭据宣称可用：验证它们的绑定/归属与结构，业务可用性仍由现有规则决定。现有 fixture 可继续使用自动生成的开发 key；恢复流程严格要求匹配的现有 key。每日运行任务由同一运维门执行，取消和失败有界，不因定时器运行产生生产验收结论。

## Verification

- Docker Go 1.27.2：`go test -race -coverpkg=./... ./...`、`go vet ./...`、`govulncheck ./...`；无竞态/失败/已知漏洞。
- `uv run --with jsonschema scripts/contract-schema.py`：新旧请求、样例及回归检查通过。
- Docker 两次 `scripts/build/package.sh`，比较 SHA256；归档运行 hub/hubd 与 fixture 验证。
- Docker 运行 benchmark/drill；故障和成功证据仅非敏感统计。生产验收保持未完成。

## Auto Run Result

Status: done（仅用户确认的平台无关fixture/Docker范围）。

已交付：27操作严格CLI、运维冻结/完整审计排空、UTC一年保留、每日双库一致备份、独立密钥绑定、关闭恢复/显式复核开放、初始化前锁、安全停机、双架构可复现归档与可复跑测量/故障演练。

审查结果：18 findings全部逐项记录。共享根因合并后修复8个medium条目、1个low手册条目；high修复0。延后2个medium未验证的容量性能疑点。拒绝4条：生产连续24h RPO属于用户排除范围；超大inventory的1MiB限制及不可取消门等待为本轮少见且需要非直接协议/门机制改造的low；广义产品完成读法被用户明确fixture/Docker选择否定。完整证据见Review Triage Log与[验证记录](epic-3-validation.md)。

followup_review_recommended: false。初次修复数量达到两个medium的复查阈值，但所有已复现修复点均经过针对回归与最终全量验证，未能指出具体仍未验证的修复风险，按规则设为false；容量性能疑点继续列在deferred，未以其替代修复风险。

验证：Docker Go1.27.2全包race PASS，合并覆盖率80.7%；vet PASS、govulncheck无漏洞；27操作/54样例/10契约回归PASS；Python分页3测试PASS；双次双架构归档字节一致且白名单/权限/checksum通过；amd64原生Docker和arm64 binfmt模拟的14项演练均PASS。100样本/10并发p95为121.723ms，并发备份126.377ms；故障注入保留100样本，10成功/90失败并按预期退出1。隔离Compose健康查询通过。npm audit执行后因无lockfile返回ENOLOCK，不能记为通过；package.json无依赖，本轮未引入无关lockfile。git diff --check通过。

剩余限制：大历史事件/备份扫描开销未验证；完整recovery.open清单须装入1MiB；排空门等待不即时取消。生产OpenBao/AppRole/Transit与独立材料、真实Seller授权/刷新/当前值复核、Ubuntu/systemd、双架构实机、系统/edge拒绝采集、健康真实KMS性能、连续生产RPO和人工RTO均未完成。完整首期及原Epic3生产门槛未改成通过。

交付方式：在隔离工作区的codex/epic-3完成本地提交，不推送；原codex/epic-2工作区及其未跟踪材料保留。

### 文件与用途

| 文件 | 变更说明 |
| --- | --- |
| [.gitignore](../../.gitignore) | 忽略Python临时缓存 |
| [Dockerfile](../../Dockerfile) | 非root fixture运行目录与隔离演练镜像 |
| [Dockerfile.operations](../../Dockerfile.operations) | 非root fixture运行目录与隔离演练镜像 |
| [README.md](../../README.md) | fixture范围说明与可复跑运维手册 |
| [_bmad-output/implementation-artifacts/epic-3-context.md](epic-3-context.md) | 本轮范围、实现/审查记录或逐Story验收证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/benchmark-fault.json](epic-3-evidence/benchmark-fault.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/benchmark-maintenance.json](epic-3-evidence/benchmark-maintenance.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/benchmark.json](epic-3-evidence/benchmark.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/drill-arm64-emulated.json](epic-3-evidence/drill-arm64-emulated.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/drill.json](epic-3-evidence/drill.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-evidence/releases.json](epic-3-evidence/releases.json) | 最终fixture测量/故障/恢复/归档的非敏感原始证据 |
| [_bmad-output/implementation-artifacts/epic-3-validation.md](epic-3-validation.md) | 本轮范围、实现/审查记录或逐Story验收证据 |
| [_bmad-output/implementation-artifacts/spec-epic-3-development.md](spec-epic-3-development.md) | 本轮范围、实现/审查记录或逐Story验收证据 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/CLI-CONTRACT.md](../planning-artifacts/contracts/cli-v1/CLI-CONTRACT.md) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/backup.schema.json](../planning-artifacts/contracts/cli-v1/backup.schema.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/examples.json](../planning-artifacts/contracts/cli-v1/examples.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/recovery.schema.json](../planning-artifacts/contracts/cli-v1/recovery.schema.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/request.schema.json](../planning-artifacts/contracts/cli-v1/request.schema.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/retention.schema.json](../planning-artifacts/contracts/cli-v1/retention.schema.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/cli-v1/validation.json](../planning-artifacts/contracts/cli-v1/validation.json) | 27操作的严格输入输出契约及静态样例验证 |
| [_bmad-output/planning-artifacts/contracts/operations/OPERATIONS-CONTRACT.md](../planning-artifacts/contracts/operations/OPERATIONS-CONTRACT.md) | 一年保留、冻结、独立密钥、关闭恢复及测量口径 |
| [cmd/hub/operations_test.go](../../cmd/hub/operations_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [cmd/hub/response.go](../../cmd/hub/response.go) | 运维DTO、严格契约和无秘密响应验证 |
| [cmd/hubd/daemon.go](../../cmd/hubd/daemon.go) | 离线恢复、启动核验、持久每日任务和排空停机 |
| [cmd/hubd/operations_linux_test.go](../../cmd/hubd/operations_linux_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [compose.yaml](../../compose.yaml) | 非root fixture运行目录与隔离演练镜像 |
| [deploy/fixture-backup.json](../../deploy/fixture-backup.json) | 仅fixture的systemd运行/可选定时备份示例 |
| [deploy/systemd/api-key-hub-fixture-backup.service](../../deploy/systemd/api-key-hub-fixture-backup.service) | 仅fixture的systemd运行/可选定时备份示例 |
| [deploy/systemd/api-key-hub-fixture-backup.timer](../../deploy/systemd/api-key-hub-fixture-backup.timer) | 仅fixture的systemd运行/可选定时备份示例 |
| [deploy/systemd/api-key-hub-fixture.service](../../deploy/systemd/api-key-hub-fixture.service) | 仅fixture的systemd运行/可选定时备份示例 |
| [docs/operations.md](../../docs/operations.md) | fixture范围说明与可复跑运维手册 |
| [internal/adapters/backup/backup.go](../../internal/adapters/backup/backup.go) | 两库快照验证、独立key绑定、原子发布与关闭恢复 |
| [internal/adapters/backup/backup_test.go](../../internal/adapters/backup/backup_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [internal/adapters/backup/publish_linux.go](../../internal/adapters/backup/publish_linux.go) | 两库快照验证、独立key绑定、原子发布与关闭恢复 |
| [internal/adapters/backup/publish_other.go](../../internal/adapters/backup/publish_other.go) | 两库快照验证、独立key绑定、原子发布与关闭恢复 |
| [internal/adapters/local/listen_linux.go](../../internal/adapters/local/listen_linux.go) | 数据库/key初始化前的Linux实例锁 |
| [internal/adapters/local/listen_other.go](../../internal/adapters/local/listen_other.go) | 数据库/key初始化前的Linux实例锁 |
| [internal/adapters/protector/fixture.go](../../internal/adapters/protector/fixture.go) | 只读取已存在安全独立key的恢复材料入口 |
| [internal/adapters/sqlite/operations.go](../../internal/adapters/sqlite/operations.go) | 恢复控制字段迁移、快照完整性/约束和事务保留 |
| [internal/adapters/sqlite/operations_test.go](../../internal/adapters/sqlite/operations_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [internal/adapters/sqlite/repository.go](../../internal/adapters/sqlite/repository.go) | 恢复控制字段迁移、快照完整性/约束和事务保留 |
| [internal/adapters/sqlite/repository_test.go](../../internal/adapters/sqlite/repository_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [internal/application/boundary.go](../../internal/application/boundary.go) | 运维排空门、权限复核、恢复开放、健康与持久缺口 |
| [internal/application/operations.go](../../internal/application/operations.go) | 运维排空门、权限复核、恢复开放、健康与持久缺口 |
| [internal/application/operations_test.go](../../internal/application/operations_test.go) | 持久恢复、边界、安全拒绝或分页的回归验证 |
| [internal/application/read.go](../../internal/application/read.go) | 运维排空门、权限复核、恢复开放、健康与持久缺口 |
| [internal/application/store.go](../../internal/application/store.go) | 运维排空门、权限复核、恢复开放、健康与持久缺口 |
| [internal/buildinfo/build.go](../../internal/buildinfo/build.go) | 发布源码绑定构建版本 |
| [internal/contract/v1/contract.go](../../internal/contract/v1/contract.go) | 运维DTO、严格契约和无秘密响应验证 |
| [internal/contract/v1/operations.go](../../internal/contract/v1/operations.go) | 运维DTO、严格契约和无秘密响应验证 |
| [internal/contract/v1/validation.go](../../internal/contract/v1/validation.go) | 运维DTO、严格契约和无秘密响应验证 |
| [scripts/build/package.sh](../../scripts/build/package.sh) | 锁工具链双架构可复现归档及归档测试镜像 |
| [scripts/build/prepare-operations-image.sh](../../scripts/build/prepare-operations-image.sh) | 锁工具链双架构可复现归档及归档测试镜像 |
| [scripts/contract-schema.py](../../scripts/contract-schema.py) | 机器schema、27操作样例和回归检查 |
| [scripts/operations/benchmark.py](../../scripts/operations/benchmark.py) | 隔离完整CLI测量、故障统计和关闭恢复演练 |
| [scripts/operations/common.py](../../scripts/operations/common.py) | 隔离完整CLI测量、故障统计和关闭恢复演练 |
| [scripts/operations/drill.py](../../scripts/operations/drill.py) | 隔离完整CLI测量、故障统计和关闭恢复演练 |
| [scripts/operations/test_drill.py](../../scripts/operations/test_drill.py) | 持久恢复、边界、安全拒绝或分页的回归验证 |
