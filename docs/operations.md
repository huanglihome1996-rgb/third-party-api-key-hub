# 平台无关运行与恢复

本手册针对用户确认的 Epic 3 本地 fixture 范围。Go 1.27.2、SQLite 1.60.1 和 Unix CLI 核心可在 Docker 验证；生产 OpenBao、TikTok Seller、Ubuntu/systemd、双架构实机及人工恢复验收仍待完成。fixture 服务拒绝非 fixture 启动。

## 可复现归档

Windows 不安装 Go，使用已有 Docker 缓存。先在仓库根取得完整 revision，再传给容器（linked worktree 的 `.git` 指向 Windows 主仓库，容器不能据该路径读取 Git）：

```powershell
$hubRevision = git rev-parse HEAD
docker run --rm -e "HUB_SOURCE_REVISION=$hubRevision" -v "${PWD}:/src" -v epic1-go-mod:/go/pkg/mod -v epic1-go-build:/root/.cache/go-build -w /src golang:1.27.2 bash scripts/build/package.sh
```

输出 `dist/releases/api-key-hub-0.2.0-fixture-linux-{amd64,arm64}.tar.gz` 和 SHA256SUMS。归档包含 hub/hubd、模块哈希、release.json、fixture unit、手册和 Python 标准库演练工具；无业务数据库、开发 key 或凭据。release.json 记录完整 revision 与实际 Go 源码树哈希，未提交源码不被伪装成 revision 对应源码；归档二进制的构建版本同时绑定版本号和源码树hash，跨构建备份不能直接混拼。归档固定排序/权限/时间和 owner；相同源码、模块、工具链和随包文件重复构建应字节一致。Python 只供演练工具使用，不是 hub/hubd 运行依赖。

解包后先运行 `sha256sum -c SHA256SUMS`；交叉编译成功不证明目标运行。systemd 文件仅是 `hubd-fixture` 隔离账户的示例，账户/目录安装须由目标主机运维核验。daemon 自带每日备份/保留时，不同时启用可选 backup.timer；该 timer 不构成 systemd 实际验收证据。此范围没有生产 unit 或生产身份策略。

## 在线运维

向 `hub --socket /state/instance/hub.sock` 的 stdin 发送单个 JSON。state-dir的父目录必须已存在且对服务可写：初始化前的同级instance.lock保护新目录恢复与运行实例，容器卷因此挂载到父目录/state，实例位于/state/instance。以下操作须由 fixture 服务 UID 发起并具 manage；客户不能指定任意备份或清理路径。

```json
{"version":"contract/v1","operation":"operations.backup","payload":{}}
```

成功返回 `backup_id`、`revision`、`audit_gap`、`created_at` 和 `mode`。完整备份原子发布在 state-dir/backups/backup_id，固定包含两库与 manifest.json，未完成目录不视为备份。包保留已有审计缺口；取密依旧先检查完整映射、全部必需值与权限，没有跨请求明文缓存。

daemon按已校验manifest的最新共同快照时间计算每日到期，启动补跑已到期任务，重启不重新延后24小时。缺/损坏/过期包产生固定枚举告警，并在health的operations_degraded体现；恢复关闭阶段跳过自动维护。health的audit_degraded单独反映审计缺口。

```json
{"version":"contract/v1","operation":"operations.retention","payload":{}}
{"version":"contract/v1","operation":"recovery.status","payload":{}}
```

审计 cutoff 取 UTC 日历周年与 now−366天两者中更早的时间，只清理严格早于 cutoff 的事件，恰好边界保留；损坏事件、异常未来时间或时钟倒退安全失败。失败告警仅固定安全类别，健康状态降级，业务结果不回滚。服务未收到的系统连接拒绝须由生产部署采集，不能由当前服务审计代替。

手工备份或清理的真实依赖故障同样置 operations_degraded，并由 daemon 输出固定 `event=operations_dependency_failed count=<数字>`；权限拒绝不被记为依赖故障。既有实例启动先验证全部持久逻辑凭据与独立key绑定，错误key安全拒绝；既有业务库丢失审计库时会持久记录audit_gap，重建空审计库不代表恢复历史。

## 独立密钥与安全恢复

开发 key 必须独立于业务包保管，0600 文件与0700目录；manifest 仅含非秘密指纹/版本引用。fixture 新实例的默认开发 key 在 state-dir/fixture.key；也可使用 `--fixture-key-file` 指向现有独立 key。备份不会复制该 key。生产 OpenBao 的恢复材料必须由独立权限、版本和身份流程管理，本地开发 key 演练不验证该流程。

只恢复到不存在的新目录，不能覆盖活动实例。恢复先验证完整文件白名单、哈希、SQLite完整性/schema和独立 key 的实际绑定解密，再发布恢复目录，状态持久关闭。CLI 的秘密查询在关闭阶段整笔失败。

```sh
hubd --fixture --restore-from /backup/backup-id --fixture-key-file /recovery-material/fixture.key --state-dir /new-restored-state
hubd --fixture --state-dir /new-restored-state --fixture-key-file /recovery-material/fixture.key
```

第一条只恢复并退出。第二条启动已恢复服务。缺 key 不会生成新材料；错误 key、损坏包、符号链接、已存在目标目录安全拒绝。备份目录及独立材料须处于受控本地主机权限边界；哈希提供损坏检测，不代替受信任备份来源或防恶意改写的签名。

先用 recovery.status 取得当前 revision，然后用 credential.status 逐页取得全部当前逻辑凭据的 generation：显式 limit=100，offset 从0开始，每页累加实际返回条目数，直到总数等于 total；不能仅使用默认第一页。所有页的 revision 和 total 必须保持一致，完成后再次读取 recovery.status，确认 revision 未变化；变化时丢弃本轮清单并重新核对。核对当前账户、完整归属和虚构当前值，精确列出全部凭据版本，再发送：

```json
{"version":"contract/v1","operation":"recovery.open","payload":{"expected_revision":9,"accounts_verified":true,"ownership_verified":true,"values_verified":true,"credentials":[{"scope":"app","app_id":"fixture-app","type":"client_secret","expected_generation":1},{"scope":"shop","app_id":"fixture-app","external_shop_id":"fixture-shop","type":"shop_token","expected_generation":1}]}}
```

示例 revision 必须用实际当前值；不要盲目复用。服务复核完整覆盖、当前 generation、绑定和安装的必需组合；过时版本、撤权或缺链仍保持关闭。开放持久化成功后才能取密；停用/过期目标仍拒绝，不以恢复开关解除凭据自身停用。未结平台 operation、旧 refresh Token 与真实平台值的复核在 Epic 2/生产范围，当前 fixture 没有这些能力，不宣称已验证。

完整 recovery.open 清单仍须放在同一请求中，保持1 MiB输入上限；分页采集不扩大这个容量，超限时继续关闭并停在人工处理点。运维门等待当前请求与审计排空期间不可取消，CLI超时不证明服务动作未执行；用非敏感状态和已校验包复核结果。大清单、大历史容量及生产连续RPO尚未验证。

## 备份空间与暂存目录的人工维护

此 fixture 不自动删除旧备份。清理前停止 fixture 服务、可选 timer 和演练进程，确认实例退出且没有恢复或备份正在运行；运行中不清理 `.incomplete-*` 或同父目录下 `.restore-*`。先保留独立受控密钥材料，并对准备保留的恢复点逐份核验文件白名单、manifest绑定和哈希，使用对应构建与独立匹配key在新的隔离目录完成关闭恢复验证；仅有名称、时间或manifest存在不能证明可恢复。

在确认至少保留所需独立可恢复点及其受控副本后，人工按明确的绝对目录逐项清理确实过期的备份和停机后确认遗留的暂存目录；不使用跨父目录通配清理，不删除活动状态目录、数据库、实例锁或任何独立key材料。保留审计要求和恢复点覆盖范围仍由运维负责；释放空间后重新核验保留包再启动fixture，检查health和固定枚举告警。磁盘容量规划、备份份数策略与生产连续RPO须另做目标平台验收。

## Docker 演练

```powershell
docker build -f Dockerfile.operations -t api-key-hub-operations:epic3 .
docker run --rm --network none api-key-hub-operations:epic3 --requests 100
docker run --rm --network none api-key-hub-operations:epic3 --requests 100 --maintenance
docker run --rm --network none --entrypoint python api-key-hub-operations:epic3 /tools/drill.py
```

镜像以 UID10001 运行，只用虚构值和隔离临时状态。benchmark 真正启动每个 hub CLI 进程，10 并发请求，计时包括 CLI、服务、解密与完整输出校验；不做主样本暖机排除，nearest-rank `ceil(0.95*n)`，全部/成功/失败样本各自统计，慢请求不剔除。maintenance 模式并发执行备份。报告只写延迟、错误枚举和环境声明，无秘密响应；目标2秒不改变。

可用 `--fault-after 10` 在请求期间停止隔离 daemon，验证失败样本仍完整统计。该故障场景应返回非零及 `status: FAIL`，不能计为健康主指标达标；不会停止任何既存实例。

已按前文用缓存构建归档时，可直接用归档准备测试镜像，避免镜像 builder 再次下载 Go 模块：

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.27.2 bash scripts/build/prepare-operations-image.sh
docker build -t api-key-hub-operations:epic3 dist/operations-image
```

此路径实际演练归档里的 amd64 二进制和工具。修改源码或工具后必须先重建归档；arm64 二进制仍须另在明确的目标/模拟环境执行。

drill 经在线备份、缺/错 key拒绝、恢复、两次关闭重启、过时复核拒绝、显式开放、正确业务组合和停用重启拒绝。以冻结快照时间到故障时刻量化 fixture RPO，以故障至开放的连续单调时间量化自动演练耗时；不会暂停计时排除复核/失败检查。可用 `--output /reports/report.json` 保存非敏感报告。

生产验收仍须：健康 OpenBao 条件、目标 Linux/两个实机架构、真实 Seller当前值复核、受控独立恢复材料、每日连续调度/过期检测、系统拒绝和告警实际采集，以及包含人工复核的 RTO≤4h/RPO≤24h。自动秒级 fixture 演练不代表人工生产 RTO 达标。
