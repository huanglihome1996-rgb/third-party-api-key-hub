---
title: Epic 3 平台无关代码与 Docker 验证记录
status: local-scope-pass
updated: 2026-10-10
production_acceptance: incomplete
---

# Epic 3 本轮交付证据

用户已确认本轮只完成平台无关代码与 Docker 验证。以下结果来自 `codex/epic-3` 隔离工作区的最终候选；原 Epic 3 的生产门槛继续未完成，Epic 2 的授权/刷新也未被本轮实现取代。

## 逐 Story 结果

| Story | 本轮已实现及验证 | 原生产验收仍缺少的证据 |
| --- | --- | --- |
| 3.1 契约 | [运行契约](../planning-artifacts/contracts/operations/OPERATIONS-CONTRACT.md)、27 操作机器契约明确 UTC 保留、冻结、独立密钥、关闭恢复及测量口径 | SQ-3/4/5 的真实 OpenBao、部署、采集、人工演练定稿与实证 |
| 3.2 发布包 | Go 1.27.2，amd64/arm64 hub/hubd；两次构建归档 SHA256 完全相同；固定文件白名单/权限/内部校验和通过；两架构拒绝非法输入和非 fixture 启动 | Ubuntu 24.04 的安装/systemd 及两架构实机运行 |
| 3.3 保留/告警 | 一年 UTC 严格边界、闰日、损坏/未来事件/时钟倒退安全拒绝；持久审计缺口；固定枚举告警/健康降级 | 系统/edge 拒绝采集，真实日志和告警链路，生产历史事件/存储验证 |
| 3.4 备份 | 运维门排空业务及拒绝审计；SQLite VACUUM INTO 双库快照，manifest/hash/schema/配置/构建/密钥绑定，私有 staging 原子发布；权限撤回/依赖故障保留旧包；每日计划跨重启及清理重试测试通过 | 生产连续每日执行、异盘保护与独立 OpenBao 版本/身份材料 |
| 3.5 关闭恢复 | 缺/错 key、损坏包、链接/额外文件/已有目标拒绝；新目标持久关闭；管理修复不开放，显式复核 revision/完整凭据集合才开放；停用重启后仍拒绝 | 真实 Seller 当前值/账户/归属复核及未结操作/旧 refresh 的恢复资格 |
| 3.6 10 并发 | 100 个完整 CLI 样本，全部 p95 121.723 ms；并发 3 次备份时 p95 126.377 ms，均 100 成功；故障注入保留 100 样本并明确 FAIL | 健康隔离真实 KMS、目标 Linux 及生产数据组合下 p95≤2s |
| 3.7 恢复演练 | amd64 和模拟 arm64 均通过 14 项 CLI 演练；amd64 fixture RPO 0.040761 s，自动恢复 0.2392 s；arm64 模拟 RPO 0.296339 s，自动恢复 5.2673 s | 连续生产 RPO≤24h，以及包含人工复核/材料获取/平台等待的 RTO≤4h |
| 3.8 证据包 | 本文件、[操作手册](../../docs/operations.md)、fixture systemd 样例与下列可追溯报告 | 首期 Linux、安全、真实平台及全部 SQ 门槛；完整首期仍未验收 |

## 验证命令与结果

在 Docker Go 1.27.2、缓存模块/构建卷内运行：

```text
go test -race -coverpkg=./... -coverprofile=dist/verification/coverage.out ./...
go test -race -v ./...
go tool cover -func=dist/verification/coverage.out
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
uv run --with jsonschema scripts/contract-schema.py
```

全包 PASS，无竞态；合并语句覆盖率 80.7%；vet PASS，govulncheck 为 `No vulnerabilities found.`。机器契约 27 操作、54 样例、10 回归 PASS。`uv run python -m unittest discover -s scripts/operations -p 'test_*.py' -v` 的3个分页测试通过。日志及覆盖率保存在本地 `dist/verification/`。提交前 `npm audit --json` 返回 ENOLOCK；当前 package.json 无依赖/lockfile，不能记为 npm 审计通过，未因此生成无关锁文件。

两次 `scripts/build/package.sh` 的归档字节一致。检查 tar 文件集、owner/group/mtime、0755/0644 权限和全部内部 SHA256；归档不含数据库、key 或凭据。`release.json` 将完整基线 revision 与实际 Go 源码树 hash 分开记录；构建版本绑定源码树，未提交代码不伪装为基线源码。

| 架构 | SHA256 | 执行环境 |
| --- | --- | --- |
| amd64 | `ea46840f4ca244bc06405e2430341a54a78f3ff25b54cc4e5e80e05ba3772f94` | Docker Desktop Linux x86_64，UID10001 |
| arm64 | `9ca15371aa062fa0caaa2d1299f7b5a966b2caa1778f0b7b6357d1ac49d389f0` | arm64 ELF，经 binfmt 模拟；Python/runtime 为 x86_64，UID10001 |

Docker 运行使用最终归档生成的 `api-key-hub-operations:epic3`，image ID `sha256:45987df00789f2290a3491e8499dd8040c480542984a2ca1a4f67805bea2086d`，`--network none`，各演练使用自己的临时状态，未接触原实例。正常和维护测量包含 CLI 进程/解密/完整输出校验、10 个 query worker、全部 100 个样本，不剔除冷/慢/失败样本；nearest-rank 为 `ceil(0.95*n)`。故障场景在请求索引10终止自己的 daemon，退出1，10成功/90失败，100个延迟全部保留；该 FAIL 是故障统计验收结果，不是健康性能达标证据。

隔离 Compose 项目 `epic3-verification` 使用独立fixture卷及operations镜像覆盖，验证UID10001/只读容器/禁网下的CLI健康响应；之后停止容器并移除专用卷。原Epic1/2工作区和实例未被修改。

报告可直接复核二进制 SHA256、ELF 架构、UID、时间与全部样本：

- [正常 benchmark](epic-3-evidence/benchmark.json)、[并发维护 benchmark](epic-3-evidence/benchmark-maintenance.json)、[预期失败 benchmark](epic-3-evidence/benchmark-fault.json)。
- [amd64 恢复演练](epic-3-evidence/drill.json)、[arm64 模拟恢复演练](epic-3-evidence/drill-arm64-emulated.json)。
- [归档和启动拒绝检查](epic-3-evidence/releases.json)。

## Matrix 测试审计

以下覆盖测试已在 `go-test-verbose.log` 中实际运行且 PASS，无跳过；演练命令成功退出0，故障命令按预期退出1。

| Intent matrix 场景 | 已执行覆盖 |
| --- | --- |
| 在线备份/审计排空/失败保留 | TestBackupDrainsDeferredBusinessAndBoundaryAudit；TestFailedBackupKeepsPublishedPackageAndRevocation；TestRealAuditFailurePersistsGapAndAppearsInSnapshot；并发维护 benchmark |
| UTC一年/边界/损坏/异常时钟 | TestRetentionUTCStrictBoundaryAndFailClosed；TestRetentionPermissionRevocationRollsBack |
| 完整包/独立key/关闭恢复/链接/绑定 | TestBackupRestoreIntegrityKeyBindingsAndClosedRestart；TestBackupRejectsExtraFilesAndCipherBindingEvenWithUpdatedHashes；两架构 drill |
| 当前版本/撤权/缺链/缺项/显式开放 | TestRecoveryClosedAcrossEngineRestart；TestRecoveryOpenRechecksPermissionAndKeepsClosed；TestRecoveryReviewRequiresCompleteChainsAndBindings；TestIntermediateSnapshotRestoresClosedAndRequiresRepairBeforeOpen；TestRestoredDisabledCredentialRemainsDisabledAfterOpen |
| 关闭重启/初始化前锁/第二实例 | TestInitializationLockAndExplicitMissingKeyNeverTouchStorage；TestActualV1MigrationRecoveryAndGapRemainAfterRestart；两架构 drill |
| 完整CLI/故障样本/fixture口径 | 正常、并发维护与预期失败 benchmark；两架构14项 drill |

TestDailyScheduleSurvivesRestartDetectsInvalidAndSkipsClosed 还验证持久计划、过期/损坏、未发布临时目录不误报，以及备份已成功但清理失败时仍重试。恢复没有平台调度能力，不能借此声称真实平台调度已被关闭或安全重启。

## 独立审查修复

四层审查已完成；18项结论逐项保存在spec的Review Triage Log。8个medium修复根因和1个low手册修正均已落实；2个容量性能疑点保留为未验证，4个结论按证据/范围或fixture日常影响拒绝。复查未发现需要额外审查的具体残留修复风险。

新增回归测试实际PASS：TestAdmissionRejectsWrongExistingKeyAndPersistsLostAuditGap、TestReplacingIndependentKeyCannotPublishUnrestorableBackup、TestRestoreRejectsDuplicateIdentitiesOwnersAndInvalidRowRevisions、TestSnapshotRejectsMissingConstraintsAndUnsupportedIndexes、TestManualDependencyFailureProducesOnlyFixedAlert、TestManualOperationsDependencyFailureDegradesAndCountsSafeAlerts、TestRetentionIndependentCalendarUTCExamplesAndYoungEvent。备份恢复测试还逐字段比较源审计、快照审计和恢复审计；Python分页测试覆盖205条完整采集及版本变化/截断拒绝。修复后重新运行全量Go/契约/双次构建及两个架构演练，以上报告均为此次最终结果。

## 尚未完成

生产 OpenBao/AppRole/Transit 版本及独立身份材料；真实 TikTok US Seller 授权/刷新和恢复复核；Ubuntu/systemd、真实双架构、root/服务 UID 权限策略；系统拒绝/告警采集；健康真实 KMS 的性能；连续生产 RPO 和人工 RTO。规划原门槛不改为通过。

本轮规模限制：recovery.open 的全部版本引用必须能装入既有1MiB请求，尚无批量复核协议；维护门排队等待不会即时响应context取消，进入后才安全拒绝取消请求。大历史审计量及大备份历史量的扫描开销尚未验证，不能从100次fixture样本扩称生产容量或deadline达标。

## 后续审查修复证据

以上数据保留为原 Epic 3 交付的历史记录。2026-10-09 的 party-mode 审查随后发现既有空业务/审计库可被初始化放行的问题，并实测150包历史完整扫描超过两分钟。后续 admission、恢复日志校验与有界并发扫描修复，以及新的明确规模容量结果，统一见[审查修复验证](review-remediation-validation.md)和[原审查及处置](../party-mode/reviews/2026-10-09-epic-3-code-review.md)。历史“尚未验证”的容量结论由该记录中的实测范围补充；任意历史量和真实生产验收仍未证明。
