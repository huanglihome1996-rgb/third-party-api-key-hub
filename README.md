# Third-Party API Key Hub

用于统一对接第三方平台并管理其 API 凭据的项目基础目录。

## 当前状态

- 已初始化 Git 仓库
- 已安装 BMAD Method 6.12.0（BMM 模块）
- 已配置 Codex 技能到 `.agents/skills`
- BMAD 产物默认输出到 `_bmad-output`
- 已形成首期 PRD、架构、SPEC 和 34 条规划任务；业务实现和实际平台/Linux/KMS 验收尚未开始，整体开发就绪 **FAIL**。

## 现行文档入口

从 [首期 SPEC](_bmad-output/specs/spec-third-party-api-key-hub/SPEC.md) 开始，必须共同读取其 companions：

- [PRD](_bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/prd.md) 和 [技术补充](_bmad-output/planning-artifacts/prds/prd-third-party-api-key-hub-2026-10-08/addendum.md)：产品范围和验收。
- [架构契约](_bmad-output/planning-artifacts/architecture/architecture-third-party-api-key-hub-2026-10-08/ARCHITECTURE-SPINE.md) 及其状态、兼容、待处置结果 companions：已采用的实施规则。
- [实施门槛](_bmad-output/specs/spec-third-party-api-key-hub/implementation-gates.md)：尚缺的契约与实际证据。
- [Epic/Story 规划](_bmad-output/planning-artifacts/epics.md) 和 [CE 校验](_bmad-output/planning-artifacts/CE-VALIDATION.md)：规划覆盖与开发就绪判断。

文档 `final` 表示基线定稿，不表示功能实现、联调或运行目标已通过；`docs/project-brief.md` 是历史输入。六项审查修订的依据与复核见 [修订收尾记录](_bmad-output/party-mode/reviews/2026-10-08-document-repair.md)。

## 开始使用

1. 在项目目录打开 Codex 或其他已配置的 AI 工具。
2. 使用 `bmad-help` 技能查看适合当前阶段的 BMAD 工作流。
3. 复制 `.env.example` 为 `.env.local`，填入本地开发所需配置。

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
| `src/` | 后续第三方平台适配器和密钥管理代码 |

