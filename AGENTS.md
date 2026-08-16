# Gpi 项目规则（AGENTS.md）

本文件记录本项目约定，供开发/协作时遵循。

## Go 代码约定

- **import 分组（三组）**：每个 go 文件的 import 固定分 3 组，组间空行分隔，组内按字母序：
  1. 系统/标准库包（`context`、`fmt`、`os` 等）；
  2. 第三方包（`github.com/spf13/cobra`、`go.uber.org/zap` 等）；
  3. 项目内部包（`github.com/acmestack/gpi/...`）。
  统一用 `goimports -local github.com/acmestack/gpi -w <file>` 自动分组（工具已装在 `~/go/bin/goimports`）。不要手动把第三方和内部包混在一组。
- **生成/新增 Go 文件后必须格式化**：每次生成或新增 `.go` 文件后，立即运行 `gofmt -w <file>` 或 `goimports -local github.com/acmestack/gpi -w <file>` 格式化，确保缩进、空行、import 分组符合规范。
- **provider.go 文件结构顺序**：每个 cloud provider 的 `provider.go` 必须按以下顺序组织：
  1. `var logger = logging.WithName("<name>")` — 包级日志器
  2. `const CloudName = "<name>"` — 云名称常量（**必须在 provider.go 中，不在 client.go 中**）
  3. `func init()` — 注册 `cloud.Register(Provider{})` 和 `cloud.RegisterFactory(...)`
  4. `type Provider struct { ... }` — Provider 类型定义
  5. `func NewProvider(...)` 及其他方法
  参考：`internal/cloud/aliyun/provider.go`、`internal/cloud/aws/provider.go`。
- **client.go 不放 CloudName/logger**：`client.go` 只放 `Credentials`、`Client`、`APIError` 等类型和 HTTP 方法；`logger` 和 `CloudName` 统一放在 `provider.go` 中。
- **日志约定（`internal/logging`）**：
  - 后台/守护诊断日志用 `logging.Get()` 返回的 `*logging.Logger`，级别方法直接传 key/value 对，例如 `Log.Info("launching cluster", "cluster", name, "nodes", n)`；**不要**写 `zap.String("cluster", name)` 这类字段构造。
  - CLI 面向用户的命令输出（表格、确认提示、流式进度、结果摘要）用 `logging.CLIPrintf` / `logging.CLIPrintln`，写 stdout，**不经过日志文件**；不要用 `fmt.Print*` 直出，也不要改成 `logging.Get()`。
  - `internal/logging` 包按职责拆分：`logging.go`（Logger 封装 + Get/Setup）、`config.go`（Config + 默认值）、`encoder.go`（zap core 构建）、`rotate.go`（lumberjack 轮转）、`level.go`（级别解析）、`cli.go`（CLIPrintf/CLIPrintln）。新增日志相关代码按此结构放对应文件。

## 文档版本号与日期

- 所有生成的文档/产物（csv / html / drawio / md 等）都必须带版本号，且版本号要带上日期。
- 命名格式：`basename-vN-YYYYMMDD.ext`（N 为顺序版本号，YYYYMMDD 为变更日期）。
- 每次变更不能原地覆盖，必须在原文件基础上另存为新版本文件（在原文件名上追加 `-vN-YYYYMMDD`）。
- 旧版本文件保留不动，作为版本历史。
- md 沟通记录等文档内容中也应标注当前版本号（含日期）。
- csv / html 等派生产物若因同一内容变更而改变，二者版本号应保持一致并同步更新。

### 本项目 docs/ 的特殊约定

面向用户的文档采用**中英双语，中文优先**：

- **README 放在仓库根**（GitHub 只渲染根目录 `README.md`）：
  - `README.md` — 简体中文主 README（GitHub 首页默认展示）。
  - `README.en.md` — 英文对应 README。
  - 两者顶部互相放语言切换链接（中文 README → `README.en.md`，英文 README → `README.md`）。
- **其余文档**在 `docs/` 下按语言分目录，每语言一套完整文档：
  - `docs/zh/` — 简体中文（`CONTRIBUTING.md`/`gpi-architecture.md`/`gpi-new-cloud.md`/`gpi-optimizer-extension.md`/`gpi-enhancements-over-skypilot.md`）。
  - `docs/en/` — 英文对应全套（同名文件）。
  - 中文优先：新增/修改先做中文，随后同步英文。
- 各 README 作为该语言入口，链接到本语言 `docs/` 下的全部文档。
- `deploy/k8s/README.md`、issue/PR 模板等**代码配套说明保持单语原位**，不做双语。
- **例外**：`.github/CLA.md` 与 `.github/RELEASE_NOTES.md` 采用**同文件逐句双语**（每句中文后紧跟对应英文）。
- **同步规则**：修改中文文档后，必须同步更新英文对应文件（内容、版本号、链接一致）；反之亦然。
- **单文件双语**（同一文件内**逐句翻译：每句中文后紧跟对应英文**）：`.github/CLA.md`、`.github/RELEASE_NOTES.md`。其余 issue/PR 模板保持单语。
- 长期文档采用**版本号记录在内容中**而非文件名：

  - 文档头部写 `- **文档版本**：vN（YYYY-MM-DD）`，紧跟项目元信息（module/CLI/适用项目等），**随后直接是正文**。
  - 每次变更**原地更新**该文件并提升内容中的版本号，不另存新文件名。
  - 版本变更记录（changelog）**统一放在文档末尾**的 `## 版本记录` 区块（vN 降序），不要把 changelog 放在顶部影响正文重点。
  - 覆盖全局规则中的"另存新版本文件"条款（该条款针对一次性产物，长期文档遵循此约定）。
- **链接深度**：
  - 根 README 在仓库根——指向 `docs/zh|en/` 文档用 `docs/zh/...`/`docs/en/...` 前缀；指向仓库根资源（`examples/`、`openapi.json`、`LICENSE`、`deploy/k8s/README.md`）直接用文件名。
  - `docs/zh/` 与 `docs/en/` 在仓库根下两级——语言树内文档互链用文件名；指向仓库根资源用 `../../` 前缀。

## 沟通记录（MEMORY）

- 项目沟通记录统一维护在 `aiagents/MEMORY.md`，**每次沟通结束后都必须将本次对话的关键内容追加到该文件**，供后续对话快速恢复上下文。
- 记录内容包括：本次讨论的主题/决策、达成的结论、涉及的文件与改动要点、待办事项。
- 记录格式：在 `## 沟通记录` 下按日期分组追加一条（沿用 `### YYYY-MM-DD（主题）` 的小节形式），并同步维护末尾的"关键设计决策速查表"。
- 若某次沟通产生了新的版本号变更（docs 升版、新文件等），也在该记录中标注。

## Git 提交与推送

- 永远不要在没有用户明确确认的情况下 `git push`（包括 `--force`）。
- 提交前先展示将要推送的 commit/分支情况，等用户确认后再 push。
- 修改完成后先报告改动内容和验证结果，推送前必须询问用户。
- 每次处理完成后，把 `git diff` 的内容展示给用户，由用户判断是否可以 push。
- **每次修改完成后，先展示 diff / 改动清单，等用户明确确认后再 `git commit`**——不得直接提交。
- **所有提交必须 `-S` 签名**（GPG）：本地已配置 `user.signingkey=EE2178E827265FD0` 与 `commit.gpgsign=true`，提交命令统一用 `git commit -S`；tag 发布同样用 `git tag -S` 签名。
- git commit message 一律使用英文。
- commit message 根据当前变更的内容来写，内容可以简化，但必须体现变化的要点。

## 新特性/新 Bug 开发流程（New Feature / Bug Workflow）

开发新特性或修改 bug 时，严格按以下流程执行：

1. **同步 main**：先 `git fetch upstream && git checkout main && git reset --hard upstream/main`，随后 `git push origin main --force-with-lease` 把最新 main 同步到 fork（origin）。
2. **新建分支**：基于最新 main 创建全新分支，分支名体现特性/修复内容（如 `feature/<name>`、`fix/<name>`、`docs/<name>`）。
3. **开发**：只在该分支上工作，不要直接改 main。
4. **完成后**：展示 diff → 用户确认 → `git commit -S` → `git push origin <branch>` → 提 PR 合并到 upstream main。

## 版本发布（tag）

- 版本号命名：语义化版本 `vMAJOR.MINOR.PATCH`（如 `v0.0.1`）。
- 发布 tag 一律使用 **annotated tag**（`git tag -a <tag> -m "<message>"`），使 tag 本身携带版本说明；不要用轻量 tag（lightweight）。
- tag message 内容：首行 `gpi <tag> - <一句话定位>`，随后列出该版本的**核心特性 / 变化点**（可综合该版本代表性 commit 的 message），并注明文档、License、CLA 等约定。
- tag 指向：发布时 tag 应指向包含该版本全部改动的最新提交；若发布后又有改动需纳入，用 `git tag -f` 移动并 `git push -f` 强制更新。
- 推送：`git push cc <tag>`（必要时 `-f`）。推 tag 前同样需用户确认。
- 发布自动化：GitHub 侧由 `.github/workflows/release.yml` 在 `v*` tag 时触发（构建二进制 + GHCR 镜像 + GitHub Release，body 取自 `.github/RELEASE_NOTES.md`）。

## 版本号机制（version）

- **版本号唯一来源是 git tag**，不要在源码中手写/重复维护。
- `internal/buildinfo` 包的 `buildinfo.Version` 是唯一定义处，CLI（`gpi --version`）与 OpenAPI/Swagger（`internal/server/swagger.go` 的 `info.version`）都读取它，禁止各自硬编码。
- `buildinfo.Version` 的默认值始终等于**最新已发布 tag（去掉 `v` 前缀）**；**每次发新版本时**：发布流程通过 ldflags 把当前 tag 注入二进制（release.yml 的 `-X github.com/acmestack/gpi/internal/buildinfo.Version=$VERSION`，Dockerfile 通过 `VERSION` build-arg + ldflags 注入），同时把 `internal/buildinfo/buildinfo.go` 的默认值改到新版本号。
- 本地开发构建（`make build`）不带 ldflags，直接读默认值，无需额外处理。
- **发布后必须执行**：每次发布新版本 tag 后，需立即执行以下步骤准备下一版本开发：
  1. 基于最新 main 切出新分支（如 `chore/prepare-v0.0.2`）。
  2. 在仓库根目录创建 `VERSION` 文件，写入下一版本号（如 `0.0.2`）。
  3. 修改 `internal/buildinfo/buildinfo.go` 的 `Version` 默认值为下一版本号。
  4. 更新 `.github/RELEASE_NOTES.md` 为下一版本的空白模板。
  5. 提交、推送、合并到 main，然后删除临时分支。
- 任何地方新增"版本号"展示/输出（CLI、API、文档、产物）都从 `internal/buildinfo` 读取，不要另写常量。
