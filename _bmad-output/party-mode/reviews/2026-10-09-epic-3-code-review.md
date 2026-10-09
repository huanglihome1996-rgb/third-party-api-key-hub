---
title: Epic 3 已完成工作多角度代码审查
created: 2026-10-09
updated: 2026-10-10
status: resolved-local-scope
mode: session
scope: HEAD b184fc1 Epic 3 merge and existing Epic 1 fixture core
---

# Epic 3 已完成工作多角度代码审查

## 范围与证据

本次审查以 `b184fc1`（`Merge branch 'codex/epic-3'`）为审查基线，重点覆盖备份、保留、关闭恢复、启动 admission、Unix IPC、停机排空和 CLI 契约。Epic 3 的生产门槛仍按仓库文档标记为未完成；本审查不把 fixture/Docker 证据扩展成 OpenBao、真实 Seller、Ubuntu/systemd 或人工 RTO/RPO 验收。

本轮重新运行了以下检查：

- Docker Go 1.27.2：`go test ./...`，通过。
- Docker Go 1.27.2：`go test -race ./...`，通过。
- Docker Go 1.27.2：`go vet ./...`，通过。
- `uv run --with jsonschema scripts/contract-schema.py`，27 个操作、54 个样例和 10 个回归检查通过。
- `git diff --check`，通过；审查开始时代码工作区无未提交改动，本文件是本轮新增的审查产物。

## 房间里的争论

🔒 **Vex：** “业务库的启动判定只看 `os.Lstat(business)` 是否存在。零字节文件也会被标为 existing，随后 `sqlite.New` 会在这个文件里 `CREATE TABLE IF NOT EXISTS`，把被截断的库变成一套空库。`ValidateRestoreState` 对空状态返回成功，服务就会继续启动。重启后凭据和映射已经消失，health 还可能正常。”

😤 **Grumbal：** “这不是理论上的格式问题。`repository.go` 的初始化路径没有区分‘新建路径’和‘已有文件’，所以磁盘截断、错误挂载或人工清空都可能变成静默数据丢失。测试覆盖了版本不匹配，却没有覆盖零字节或截断数据库。”

🌶️ **Boundary：** “审计库有同一个边界。`validateAdmission` 只在审计路径不存在时记录 `audit_gap`；一个已经存在但为空的 `audit.sqlite` 会被 `NewAudit` 直接创建 `events` 表，历史审计消失却没有 gap。已有备份 schema 检查不能弥补启动时已经放行的实例。”

🎯 **Yui：** “修复应该收敛到 admission：先对所有已有数据库做只读、固定 schema/integrity 校验，再允许打开可写连接；只有路径从未存在时才初始化。不要在 `New` 内部根据文件内容猜测新实例。”

🚢 **Dana：** “这两个问题比容量疑点更值得先修。容量、1 MiB recovery 清单和不可取消的 admission 等待已经在交付记录中明确标成未验证；空库启动则会直接改变用户数据。当前 fixture 目标可以继续保留，但不能把这版当作安全重启完成。”

## Findings

### P1 — 已有空/截断业务库会被静默初始化为空状态

**证据**：`cmd/hubd/daemon.go:77-104` 仅用 `os.Lstat(business)` 设置 `existing`；`internal/adapters/sqlite/repository.go:54-87` 对任意可打开文件执行建表和迁移；`validateAdmission` 随后只验证加载出的状态。空文件会被 `sqlite.New` 变成合法 schema，空状态会通过 `ValidateRestoreState`，服务继续启动。

**影响**：业务数据库被截断、清空或错误挂载后，重启会以空状态运行；旧凭据、映射和 revision 不再可见，系统没有把启动拒绝或恢复关闭作为安全停点。该路径与“既有实例启动必须 fail closed”的运行边界冲突。

**修复建议**：把“路径不存在的新实例初始化”和“已有文件 admission”分开。已有 `business.sqlite` 必须先以只读方式通过固定 schema、integrity、metadata 行数、版本和状态校验，任何空库、截断库或异常 schema 都拒绝启动；不要先对已有文件执行 `CREATE TABLE`/迁移。新增回归测试覆盖零字节、截断、缺 metadata、缺表和损坏 journal/WAL 情形。

### P1 — 已有空/异常审计库会在无 `audit_gap` 的情况下重建

**证据**：`cmd/hubd/daemon.go:179-194` 只对审计路径不存在的情况调用 `RecordAuditGap`；`internal/adapters/sqlite/repository.go:178-187` 的 `NewAudit` 对已有文件执行 `CREATE TABLE IF NOT EXISTS events`，没有 admission schema/integrity 检查。

**影响**：零字节或被截断的 `audit.sqlite` 会被当成可用审计库打开，既有事件丢失且不会持久标记 `audit_gap`。额外表、触发器或错误约束也可能在服务已对外提供 IPC 后才暴露，导致审计完整性和启动状态不一致。

**修复建议**：已有审计库必须先调用只读 `Inspect(..., audit=true)` 或等价固定 schema/integrity 校验；校验失败时拒绝启动并输出固定安全事件。只有明确检测到文件不存在时才创建新库；缺失库继续沿用当前持久 `audit_gap` 路径。增加空文件、错误 schema、额外 trigger/index 和历史保留验证的启动测试。

### P2 — 大历史量的全量验证仍是交付后的容量风险

`Backup.Schedule` 会逐个完整验证所有已发布包，`sqlite.Inspect` 会执行 integrity/schema/load，保留和 `audit.list` 也会扫描全量事件。Epic 3 验证记录已诚实标注大历史审计量和大备份历史量尚未测量；因此这里不把它升级成已复现缺陷，但在真实生产容量测试前不能声称每日调度和 2 分钟维护预算成立。

### P2 — 生产验收边界保持未完成

README、`epic-3-validation.md` 和运维手册一致地保留了 OpenBao/AppRole/Transit、真实 Seller、Ubuntu/systemd、实机双架构、系统拒绝采集、连续 RPO 和人工 RTO 的未完成状态。fixture 通过、race 通过和模拟 arm64 通过不足以关闭这些门槛；本次审查没有发现文档把它们误报为已完成。

## 结论

当前工作具备较好的 fixture 级测试和范围诚实性，但启动 admission 的两个 P1 问题需要在下一次交付前修复并回归。修复完成后，至少重新执行全量 Go/race/vet、契约检查、空/截断数据库启动拒绝测试、审计 gap 持久化测试和 Docker 恢复演练；在此之前不建议把本轮标为安全重启或完整 Epic 3 完成。

## 修复后的多角度复核

以上引用、行号和结论保留为基线审查记录。用户随后明确要求解决全部发现；最终实现和验证见[修复规格及逐项处置](../../implementation-artifacts/spec-review-remediation.md)、[修复验证](../../implementation-artifacts/review-remediation-validation.md)。

🔒 **Vex：** “已有文件不会再走初始化建表。业务语义和独立key绑定也在v1迁移前验证；拒绝矩阵证明IPC没有开放，坏文件不会被补齐。”

🌶️ **Boundary：** “合法崩溃恢复得到了保留：已提交WAL、hot journal、RESTART pending前缀和回滚旧尾部都用了真正崩溃样本。能从格式识别的提交损坏会拒绝，但稀疏journal checksum和被抹掉的WAL最终提交标记仍有不可判别边界，不能承诺所有位损坏都能检测。”

😤 **Grumbal：** “容量疑点已经有真实FAIL：150包串行扫描超过两分钟，修复后的有界并行完整维护103秒。最终源码完整维护83.719秒（存储从tmpfs改为ext4，差异明确记录）；同规模两CPU配额CLI的backup/retention查询p95为1935/1978ms，200请求成功。此前真实FAIL及无效旧二进制报告完整保留，不能拿health就绪当完整扫描证据。”

🎯 **Yui：** “审计逐行检查与类型元数据共享都沿用每条记录的严格验证。独立复核和并发测试已确认没有缓存记录、明文或校验决策。最终全量race/81.7%覆盖率、vet、漏洞扫描、契约、Python和重新打包的14项恢复演练通过；本次明确规模的容量门已通过，接近2秒阈值的余量也保留说明。”

🚢 **Dana：** “本地代码缺陷与生产门槛分别判断。修复不替代OpenBao、真实Seller、目标systemd与双架构实机、连续RPO和人工RTO验收；这些仍未完成。”
