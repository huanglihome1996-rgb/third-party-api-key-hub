---
title: Epic 3 审查修复验证
created: 2026-10-09
updated: 2026-10-10
status: local-scope-pass
baseline_commit: b184fc1c30d1ebf12e05cb9c9b253db574636655
---

# 审查修复验证

本轮修复两个已有数据库被静默初始化的问题，并补足明确规模的历史容量证据。全部验证使用虚构数据和隔离目录，不接触现有实例、真实凭据或第三方资源。

## 回归前证据

新增 SQLite 构造器回归在修复前证明：已有零字节、缺表/metadata、额外表/trigger/index、错误约束和损坏记录会被放行；零字节和缺失 schema 还被初始化路径改写。daemon 子进程回归证明已有空/缺 schema 业务或审计库会输出 `fixture_ready` 并创建 IPC。16 字节 SQLite header 截断输入原本就被拒绝，因此不把它写成新修复的行为。

完整 v1 schema、已提交 WAL 和热 rollback journal 必须保留正确迁移/恢复能力；活动库的 admission 不能使用备份快照专用的 `immutable=1`。

## 验证结果

Go 1.27.2 / modernc SQLite 1.60.1 的启动专项测试已通过：已有业务/审计损坏库在 IPC 前拒绝；合法完整 v1 保留状态迁移；不合法 v1 的语义或密钥绑定在迁移前拒绝且 schema、version 与 bytes 保留；缺失审计 gap 在两次 daemon 启动后仍持久存在。恢复测试使用真正的进程退出留下 WAL 与热 rollback journal。

补充测试先复现四个失败，随后通过：截断已提交 WAL 丢失提交、journal 校验和损坏留下未提交 revision、journal 后续记录截断，以及合法 WAL RESTART/reuse 被过度拒绝。专项日志位于本地 `dist/verification/recovery-review-{red,green}.txt` 和 `admission-focused-green.txt`；独立审查还用实际 Go 构造器重跑了 WAL 重用、截断提交和 journal 校验和损坏。盐位损坏回退问题已修正并回归。

独立复核又用真正崩溃样本发现两种合法 WAL 尾部被误拒绝，并完成 RED→GREEN：RESTART 后 pending 当前代前缀恢复 revision1/sequence11/100apps；回滚后较短事务覆盖同代前缀恢复 revision8/sequence12/0apps。两者都使用独立 SQLite 副本恢复作为对照。盐损坏、已提交帧损坏及损坏后还有提交帧继续拒绝。最终独立复核确认无残留具体发现。

最终 WSL Debian Go1.27.2，现代 SQLite1.60.1，CGO/race 使用 GCC14.2.0：

```text
go test -race -coverpkg=./... -coverprofile=/tmp/hub-review-remediation-final-coverage ./...
go tool cover -func=/tmp/hub-review-remediation-final-coverage
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
uv run --offline --with jsonschema scripts/contract-schema.py
python -m unittest discover -s scripts/operations -p 'test_*.py'
```

最终冻结源码全部Go包通过，无竞态；合并语句覆盖率81.7%。vet通过；govulncheck输出 `No vulnerabilities found.`；静态契约27操作/54样例/10回归通过；Python12测试通过。修改Go文件gofmt无输出；git diff --check通过。coverage文件保存在本地 `dist/verification/review-remediation-coverage.out`。命令通过bash传递，保留完整 `-coverpkg=./...` 参数；不采用早期参数传递不完整的78.2%作为全项目合并覆盖率。npm audit实际返回ENOLOCK：项目package.json无依赖/锁文件，该审计不可记为通过，未生成无关锁文件。

官方Go1.27.2 Linux SDK按go.dev公布的SHA256核验，模块 `go mod verify` 全部通过；任务专用镜像仅用于模块下载，既有go.sum保留。网络获取失败和本地SDK误入包扫描已修正后重跑，不把失败尝试记为通过。

## 当前发布包与恢复演练

用最终冻结Go源码运行 `scripts/build/package.sh`，生成amd64/arm64归档；源码revision与实际源码树哈希按原发布协议分别记录。当前归档SHA256：amd64 `e32babfc7abffde8dbcf20893e0053eea8933ac27ee14c3e66bf134fadd5ce81`；arm64 `03268125973c0b1b27c30fb573194c1cb5bfab8b8984c31889ad825617715609`。[归档检查](review-remediation-evidence/releases.json)逐份确认13个内部文件哈希、文件白名单、权限/owner/mtime和来源绑定；两归档源码树均为 `e37c83265714443d16e8f0759737498c8020d21281ad8037fc7bd3424b7ae922`，与最终容量源码一致。arm64此轮仅交叉编译，不声明实机运行。

归档经 `scripts/build/prepare-operations-image.sh` 打包为 `api-key-hub-operations:review-remediation`（image ID `sha256:6c2293fc79f2adb26cc158343a9e5542e15fd41336ef4107180b5085d696a37b`），以UID10001、禁网、两CPU配额运行 `/tools/drill.py`，实际14项检查全部通过。[本轮报告](review-remediation-evidence/drill.json)记录当前二进制SHA256，fixture RPO0.057741秒、自动恢复0.287539秒；不等同于生产RPO或人工RTO。

## 容量范围

历史量、文件规模、运行环境、预算、失败样本与复跑命令以容量报告为准。任何通过结论只覆盖实测 fixture 范围；不证明任意历史量下的时延或生产能力。

受控基线已复现超限：[150 包基线](../party-mode/reviews/2026-10-09-historical-capacity-150-baseline.json) 使用 WSL2 Linux、CPU affinity 0–1 和 GOMAXPROCS=2；每包 100,002 条事件、19,624,413 bytes，150 包。Schedule 155,665.953 ms，保守完整维护 157,775.507 ms，报告和命令均 FAIL，失败证据保留。此环境是 CPU affinity，不能写成 Docker cgroup 的两 CPU 配额。

同环境与规模的[优化报告](../party-mode/reviews/2026-10-09-historical-capacity-150-optimized.json) PASS：Schedule 100,782.470 ms，backup 1,015.608 ms，retention 1,232.059 ms，保守完整维护103,030.137 ms；完整审计 Inspect 1,017.098 ms，audit.list 1,173.474 ms。完整维护按扫描+备份+保留求和，未把查询/Inspect计入自动循环。所有150包仍执行完整验证；没有历史裁剪、明文缓存或验证结果缓存。

复跑当前实现（需仓库锁定 Go 1.27.2、Linux 两个可用CPU及约3GB临时空间）：

```sh
HUB_CAPACITY_EVENTS=100000 HUB_CAPACITY_PACKAGES=150 GOMAXPROCS=2 \
HUB_CAPACITY_REPORT=/tmp/hub-capacity.json \
taskset -c 0,1 go test -count=1 -run '^TestHistoricalCapacity$' -timeout 30m -v ./internal/adapters/backup
```

[准备故障报告](../party-mode/reviews/2026-10-09-historical-capacity-setup-fault.json) 在 `TMPDIR=/proc` 的实际执行中以FAIL退出，阶段 `fixture_creation`、`setup_failed` 和 `full_validation:false`；准备失败没有被成功容量结论吞掉。

最终同一冻结源码的[完整150包报告](../party-mode/reviews/2026-10-09-historical-capacity-150-final.json)与[构建/存储来源](../party-mode/reviews/2026-10-09-historical-capacity-150-final-build.json) PASS：Schedule81,831.601ms，backup852.068ms，retention1,035.461ms，保守完整维护83,719.130ms；Inspect781.652ms，audit.list875.844ms。规模仍为每包100,002事件、19,624,413bytes、150包、affinity0–1/GOMAXPROCS2。此轮 `TMPDIR=/var/tmp` 为WSL ext4；前述基线/103秒对照为 `/tmp` tmpfs。最终数据证明当前预算，不能直接当作同存储环境的提升比例。此前 `/tmp` 空间不足发生在第118包复制准备，FAIL与 `full_validation:false` 保存在[独立失败报告](../party-mode/reviews/2026-10-09-historical-capacity-150-final-setup-no-space.json)。复跑最终环境时给前述命令补 `TMPDIR=/var/tmp`。

最终[CLI报告](../party-mode/reviews/2026-10-09-historical-capacity-cli.json)与[固定源码构建来源](../party-mode/reviews/2026-10-09-historical-capacity-cli-build.json) PASS：官方Python3.13镜像，Docker `--network none --cpus 2 --user 10001`，100,000合成历史事件、150包共3,252,716,379bytes；两次各100请求，共200/200成功，无错误，真实重叠backup13/retention10样本。backup批次p95=1,935.176ms，retention批次p95=1,978.388ms，整体p95=1,966.759ms；手工backup1,927.168ms、retention1,951.152ms。最大单样本2,102.297ms保留，没有从p95剔除。CLI只证明手工并发范围，自动完整扫描由上述Go报告单独证明。

```sh
docker run --rm --network none --cpus 2 --entrypoint python \
  api-key-hub-operations:review-remediation /tools/capacity.py \
  --events 100000 --packages 150 --requests 100
```

上述复跑使用最终发布包的同源码二进制，容量证据的快照构建版本为 `capacity-<treehash>`，发布版为 `0.2.0-fixture-<treehash>`；版本不同需成对使用，不混拼备份。原真实[优化前CLI FAIL](../party-mode/reviews/2026-10-09-historical-capacity-cli-before-streaming.json)及[暂停前CLI FAIL](../party-mode/reviews/2026-10-09-historical-capacity-cli-after-streaming-before-pause.json)完整保留。旧构建失败却误用旧二进制的报告标为[无效FAIL](../party-mode/reviews/2026-10-09-historical-capacity-cli-stale-build.json)，`measurement_valid:false`，不参与比较。最终p95余量较小，不保证不同主机、后台负载、任意历史量或真实KMS下达标。

31/100 包的原报告是中断前的探索测量，没有同样的环境限制，不作为优化前后直接比较依据。CLI 工具每种手工维护分别执行十并发查询，手工请求期限 15 秒；完整 Schedule 的两分钟预算由独立 Go 测量证明，health 或短时 CLI 结束不证明自动扫描完成。

## 校验能力边界

启动检查覆盖 SQLite 完整性、固定 schema、记录语法/语义、密钥绑定，以及可从恢复文件格式和其校验和识别的损坏。rollback journal 使用 SQLite 原生的稀疏校验和，并非全页哈希：实际隔离探针证明，未被采样的字节损坏仍可能形成合法但改变的 metadata。WAL最后提交标记损坏为零也可能无法与合法回滚pending尾部区分。当前运行库没有独立的全状态认证证据，因此不承诺识别所有位损坏或恶意重写。完整备份的 SHA256 和独立恢复材料核验继续必要；哈希也不代替受信任备份来源。

## 保留的生产门槛

OpenBao/AppRole/Transit、真实 TikTok US Seller 当前值、Ubuntu/systemd、真实双架构、系统拒绝/告警采集、连续 RPO 和包含人工核验的 RTO 均仍未验收。本轮没有这些外部材料，也没有发布或部署；它们不被 fixture 自动演练替代。
