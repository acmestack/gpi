# Gpi 项目规则（AGENTS.md）

本文件记录本项目约定，供开发/协作时遵循。

## 文档版本号与日期

- 所有生成的文档/产物（csv / html / drawio / md 等）都必须带版本号，且版本号要带上日期。
- 命名格式：`basename-vN-YYYYMMDD.ext`（N 为顺序版本号，YYYYMMDD 为变更日期）。
- 每次变更不能原地覆盖，必须在原文件基础上另存为新版本文件（在原文件名上追加 `-vN-YYYYMMDD`）。
- 旧版本文件保留不动，作为版本历史。
- md 沟通记录等文档内容中也应标注当前版本号（含日期）。
- csv / html 等派生产物若因同一内容变更而改变，二者版本号应保持一致并同步更新。

### 本项目 docs/ 的特殊约定

`docs/` 下的长期维护文档（`gpi-architecture.md`、`gpi-new-cloud.md`、`gpi-optimizer-extension.md` 等）采用**版本号记录在内容中**而非文件名：

- 文档头部写 `- **文档版本**：vN（YYYY-MM-DD）`，紧跟项目元信息（module/CLI/适用项目等），**随后直接是正文**。
- 每次变更**原地更新**该文件并提升内容中的版本号，不另存新文件名。
- 版本变更记录（changelog）**统一放在文档末尾**的 `## 版本记录` 区块（vN 降序），不要把 changelog 放在顶部影响正文重点。
- 覆盖全局规则中的"另存新版本文件"条款（该条款针对一次性产物，长期文档遵循此约定）。

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
- git commit message 一律使用英文。
- commit message 根据当前变更的内容来写，内容可以简化，但必须体现变化的要点。

## 版本发布（tag）

- 版本号命名：语义化版本 `vMAJOR.MINOR.PATCH`（如 `v0.0.1`）。
- 发布 tag 一律使用 **annotated tag**（`git tag -a <tag> -m "<message>"`），使 tag 本身携带版本说明；不要用轻量 tag（lightweight）。
- tag message 内容：首行 `gpi <tag> - <一句话定位>`，随后列出该版本的**核心特性 / 变化点**（可综合该版本代表性 commit 的 message），并注明文档、License、CLA 等约定。
- tag 指向：发布时 tag 应指向包含该版本全部改动的最新提交；若发布后又有改动需纳入，用 `git tag -f` 移动并 `git push -f` 强制更新。
- 推送：`git push cc <tag>`（必要时 `-f`）。推 tag 前同样需用户确认。
- 发布自动化：GitHub 侧由 `.github/workflows/release.yml` 在 `v*` tag 时触发（构建二进制 + GHCR 镜像 + GitHub Release，body 取自 `.github/RELEASE_NOTES.md`）。
