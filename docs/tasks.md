# Macaronic 里程碑任务清单（阶段 6）

> 本文件对应 [`development-plan.md`](development-plan.md)，拆解 M22 为
> 执行级任务。阶段 5（M20 已完成、M21 已取消）归档于
> `archive/tasks-phase5.md`。每个 T-ID 完成后将 `[ ]` 改为 `[x]`。
>
> 目标语言行为以实测探针为准；凡「已实测」标注的判断，均不得凭语法知识
> 推翻。

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M22 | 6–9 工时 | 8 / 8 |

## M22 — node 引擎

- [x] T22.1 实现前探针（已完成）：记录 node 报错定位格式、`execSync`
  失败语义、`"type": "module"` 目录下 `.js` 与 `.cjs` 的差异、Buffer 对
  四种标量与长度前缀 UTF-8 的往返、2^53 精度、`typeof` 未声明变量、
  缺文件的 `ENOENT`、NUL 字符串。验收：结论写入开发计划的实测事实表。
- [x] T22.2 引擎骨架与内嵌 codec：新增 `internal/engine/node`，`Name()`
  返回 `"node"`，生成 `run.cjs`（实测：`.js` 在 `"type": "module"` 目录下
  会以 ESM 解析而失败），内嵌 `Buffer` 版读写函数，布局与 §10 一致。
  验收：生成文本含 codec 函数与 `run.cjs` 文件名；标量四类型往返用例。
- [x] T22.3 标量注入与失败文案：prologue 读 state（缺文件报
  `macaronic: stage N: cannot read contract variable "x" (type)`）、
  epilogue 写 state（未赋值报 `... is unset at epilogue`），两处都
  `process.exit(1)`；`int` 写入非整数报含变量名的错误。
  验收：三个守卫各有用例，文案与 M20 的 shell 方言一致。
- [x] T22.4 一维数组注入：四种 list 类型的读写；`str[]` 元素含 NUL 时
  报错（与 CLI codec 规则一致）。验收：带空格 `str[]` 与四种数组往返。
- [x] T22.5 分析与遮蔽：赋值 = 写，`x++` / `x +=` = 读 + 写，其余为读；
  `let` / `const` / `var` 命中契约名报遮蔽错误并带原始行号。
  验收：table-driven 分析用例覆盖三种形态与遮蔽。
- [x] T22.6 诊断回映：`ParseDiagnostics` 解析 `file:line` 与栈帧
  `file:line:col`（实测两者都有），映射回 `.mac` 行号。
  验收：运行时错误用例报到原始 `.mac` 行号。
- [x] T22.7 `RuntimeChecker` 与跨引擎 e2e：返回 `["node"]`；新增
  node ↔ bash ↔ python 双向传值用例（标量 + 数组），缺 `node` 时
  `exec.LookPath` 守卫 skip。
  验收：`go test ./internal/engine/...` 全过且缺 node 的环境不失败。
- [x] T22.8 文档同步、质量闸门与独立提交：架构 §1 / §8 / §12、README
  能力说明与依赖段；`gofmt`、`go vet`、`go test`、`go test -race`、
  `git diff --check`、`markdownlint-cli2` 全部通过后提交。

## 依赖约束

```text
阶段 5（M20）→ M22
```

- 不新增 Go 第三方依赖；JS 侧只用 Node 标准库（`fs`、`process`）。
- 生成脚本一律 CommonJS 且名为 `run.cjs`，不读取用户工程的
  `package.json` 设置。
- `docs/archive/` 冻结；`IDEA.md` 不修改。
- 语言 e2e 一律以 `exec.LookPath` 守卫，缺解释器时 skip。
