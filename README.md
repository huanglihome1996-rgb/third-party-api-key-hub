# Third-Party API Key Hub

用于统一对接第三方平台并管理其 API 凭据的项目基础目录。

## 当前状态

- 已初始化 Git 仓库
- 已安装 BMAD Method 6.12.0（BMM 模块）
- 已配置 Codex 技能到 `.agents/skills`
- BMAD 产物默认输出到 `_bmad-output`
- 已形成首期 PRD、架构、SPEC 和 34 条规划任务。
- 2026-10-09 用户确认先交付 Epic 1 的平台无关本地核心，TikTok profile 延后到 Epic 2；已实现 Docker fixture CLI、Unix 服务、SQLite、手工导入/取密/停用恢复和非敏感审计。
- Ubuntu/systemd、生产 OpenBao/AppRole 和 TikTok 验收仍阻塞；fixture 演示不代表完整首期或生产就绪。
- Epic 3 本轮范围已确认：平台无关运行/备份/恢复核心和 Docker 验证；操作手册见 [运行与恢复](docs/operations.md)，生产验收继续独立记录。

## 现行文档入口

从 [首期 SPEC](_bmad-output/specs/spec-third-party-api-key-hub/SPEC.md) 开始，必须共同读取其 companions：

- [PRD](_bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/prd.md) 和 [技术补充](_bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/addendum.md)：产品范围和验收。
- [架构契约](_bmad-output/planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md) 及其状态、兼容、待处置结果 companions：已采用的实施规则。
- [实施门槛](_bmad-output/specs/spec-third-party-api-key-hub/implementation-gates.md)：尚缺的契约与实际证据。
- [Epic/Story 规划](_bmad-output/planning-artifacts/epics.md) 和 [CE 校验](_bmad-output/planning-artifacts/CE-VALIDATION.md)：规划覆盖与开发就绪判断。

文档 `final` 表示基线定稿，不表示功能实现、联调或运行目标已通过；`docs/project-brief.md` 是历史输入。六项审查修订的依据与复核见 [修订收尾记录](_bmad-output/party-mode/reviews/2026-10-08-document-repair.md)。

## 使用 Docker 运行本地演示

本机无需安装 Go。仅使用虚构数据；fixture 固定 App `client_secret` + shop `shop_token` 业务组合，管理 `refresh_token` 不输出。演示服务没有 TCP 端口，容器以非 root UID 10001 运行，仅通过共享卷中的 Unix Socket 访问。

```powershell
docker compose up -d --build hubd
'{"version":"contract/v1","operation":"health","payload":{}}' | docker compose run --rm -T hub
```

输入秘密只放 stdin，不放命令参数。显式隐藏交互入口为 `hub --interactive`，stdin 提供不含 value 的导入元数据；要求 Linux 控制 TTY。完整操作说明及 schema 见 [CLI 契约](_bmad-output/planning-artifacts/contracts/cli-v1/CLI-CONTRACT.md)。

停止演示（保留 fixture 卷和重启所需开发 key）：

```powershell
docker compose down
```

Epic 3 示例将实例放在卷内 `/state/instance`，同级运行锁在初始化或恢复前取得。旧 Epic 1 卷根目录的数据库/key保持原样，本轮不会自动迁移或覆盖；升级示例默认新建子目录实例。需沿用旧演示数据时，先停止旧实例，在受控可写父目录下离线迁移完整数据库和配套独立key，再启动并核对；不要移动或复制写入中的数据库。

运行验证（缓存 Go 模块和构建，不安装本机 Go）：

```powershell
docker run --rm -v "${PWD}:/src" -v epic1-go-mod:/go/pkg/mod -v epic1-go-build:/root/.cache/go-build -w /src golang:1.27.2 go test -race -coverpkg=./... ./...
docker run --rm -v "${PWD}:/src" -v epic1-go-mod:/go/pkg/mod -v epic1-go-build:/root/.cache/go-build -w /src golang:1.27.2 go vet ./...
docker run --rm -v "${PWD}:/src" -v epic1-go-mod:/go/pkg/mod -v epic1-go-build:/root/.cache/go-build -w /src golang:1.27.2 go run golang.org/x/vuln/cmd/govulncheck@latest ./...
uv run --with jsonschema scripts/contract-schema.py
```

## 凭据原则

- 不把真实 API Key、Secret、Token 或连接字符串提交到 Git。
- `.env.local` 仅用于本机开发，并已加入 `.gitignore`。
- 生产环境应接入专用密钥管理服务，并通过运行时注入凭据。

## 目录说明

| 目录 | 用途 |
| --- | --- |
| `_bmad/` | BMAD 核心与 BMM 模块 |
| `_bmad-output/` | BMAD 生成的规划与项目文档 |
| `.agents/skills/` | Codex 使用的 BMAD 技能 |
| `cmd/` | hub CLI 和 hubd 服务组装入口 |
| `internal/` | 领域、应用、共享契约、SQLite/Unix/fixture 适配器 |
| `scripts/` | 契约导出与验证 |

