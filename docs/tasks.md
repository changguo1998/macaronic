# Macaronic 里程碑任务清单（阶段 5）

> 本文件对应 [`development-plan.md`](development-plan.md)，拆解 M20–M21
> 为执行级任务。阶段 4（M17–M19）已归档于 `archive/tasks-phase4.md`。
> 每个 T-ID 完成后将 `[ ]` 改为 `[x]`。
>
> 方言行为以实测探针为准；凡「已实测」标注的判断，均不得凭语法知识推翻。

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M20 | 4–6 工时 | 6 / 6 |
| M21 | — | 已取消 |
| **M20–M21** | **约 4–6 工时** | **6 / 6** |

## M20 — 可靠性补齐

- [x] T20.1 bash 的 list prologue 响亮化：`read-list` 输出重定向到 stage
  私有临时文件（`<stageDir>/.prologue-<name>.tmp`），检查退出码，失败时报
  含 stage 与变量名的错误并 `exit 1`，成功后 `mapfile` 从该文件读入并删除
  临时文件。验收：删除 state 文件后直接跑生成的 stage 脚本，退出码非 0 且
  stderr 含变量名；正常路径数组内容不变（含带空格 `str[]`）。
- [x] T20.2 zsh 同 T20.1（`while IFS= read -r -d ''` 循环从临时文件读入）。
  验收：同 T20.1，且 `set -u` 下不引入新的硬失败。
- [x] T20.3 未赋值标量行为统一：bash / sh / zsh epilogue 在写入前检查变量
  是否存在（`[ -n "${name+x}" ]`），csh 用 `if ( ! $?name )` 守卫；四方言
  错误信息统一为含 stage 与变量名的 macaronic 级文案。验收：四方言的
  「声明写但运行时未赋值」用例退出码非 0 且文案一致；`str` 显式赋空串仍
  正常写入。
- [x] T20.4 python / go 引擎实现 `RuntimeChecker`（`python3` / `go`）。
  验收：缺失与存在两条路径的 table-driven 测试；T17.2 的「预检命令与
  `RunCommand` 的 `argv[0]` 一致」约束仍然成立（go 引擎若不满足，需在测试
  与文档中显式说明例外）。
- [x] T20.5 文档同步：架构 §8 / §12 与 README 按实现更新（list 读失败不再
  静默；未赋值标量行为；python / go 预检）。
- [x] T20.6 运行 M20 固定质量闸门并创建独立提交。
  验收：gofmt、vet、test、race、diff-check 全部通过；markdownlint 由 M21
  的 CI 承担，本地尽力执行。

## M21 — 持续集成（已取消）

不再引入 GitHub Actions 与仓库内 npm 依赖；markdownlint 由开发环境提供，
`markdownlint-cli2` 作为本地质量闸门由开发者本机执行（配置见
`.markdownlint-cli2.jsonc`）。

## 依赖约束

```text
阶段 4（M17–M19）→ M20（M21 已取消）
```

- 不新增 Go 第三方依赖；临时文件用标准库与 shell 内建实现。
- 生成脚本的方言惯用写法以实测探针为准；改动后必须重跑既有跨方言 e2e。
- `docs/archive/` 冻结；`IDEA.md` 不修改。
- 方言 e2e 一律以 `exec.LookPath` 守卫，缺方言时 skip，保持 CI 可移植。
