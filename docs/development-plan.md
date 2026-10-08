# Macaronic 开发计划（阶段 5）

> 阶段 5 主题：**可靠性补齐与持续集成**。阶段 4（M17–M19，shell 方言引擎与
> 运行时预检）已归档于 `archive/development-plan-phase4.md`。M20、M21 按顺序
> 推进，并分别独立提交。
>
> 测试约定：table-driven 单元测试 + golden/产物断言 + 跨方言端到端。
> 固定质量闸门：`gofmt -l .`（无输出）、`go vet ./...`、`go test ./...`、
> `go test -race ./...`、`git diff --check`、`npm run lint:md`
> （M21 起同一组闸门由 CI 执行）。
>
> **方言行为以实测探针为准，不凭语法知识推断。** 探针结论记录在下面的
> 「实测事实」小节。

## 背景：实测事实（实现前探针，环境 bash 5.2 / dash / zsh 5.9 / tcsh 6.24.13）

| 场景 | 实测结果 |
| --- | --- |
| `mapfile` 经进程替换读 `read-list` | 失败被吞：数组静默变空、退出码仍为 0 |
| 同一 `codec read-list` 直接执行 | `open ...: no such file or directory`，退出码 1 |
| bash epilogue 写未赋值的 `int` | codec 报 `invalid syntax`，退出码 2，信息无变量名 |
| bash epilogue 写未赋值的 `str` | 写入空字符串成功（静默，与 csh 相反） |
| tcsh epilogue 写未赋值标量 | `count: Undefined variable.`，退出码 1（响亮） |
| `RuntimeChecker` 实现者 | 仅四种 shell 方言，README 却已声明六种都 fail-fast |

三条结论驱动 M20：失败被进程替换吞掉（list 读）、未赋值标量跨方言行为不一致
（`str` 静默写空）、文档声明先于实现（python / go 预检）。

## M20 — 可靠性补齐

- **交付物**：
  - **list prologue 失败响亮化**：bash / zsh 的 `read-list` 注入不再使用进程
    替换，改为「重定向到 stage 私有临时文件 → 检查退出码 → 从文件读入 →
    删除临时文件」；失败时以含 stage 与变量名的错误报出并 `exit 1`。
  - **未赋值标量行为统一**：Bourne 系（bash / sh / zsh）epilogue 由
    `"${name-}"` 改为写入前的存在性检查 `[ -n "${name+x}" ]`；csh 增加显式
    `if ( ! $?name )` 守卫。
    四种方言在「声明了写、运行时未赋值」时给出同一句 macaronic 级错误（含
    stage 与变量名），不再出现 `str` 静默写空、也不再只报 codec 的
    `invalid syntax`。
  - **python / go 运行时预检**：两个引擎实现 `RuntimeChecker`
    （`python3` / `go`），使 README 的 fail-fast 声明成立。
  - **文档同步**：架构 §8 / §12 与 README 按实现更新。
- **依赖**：无（阶段 4 已完成）。
- **验证**：三条改动各自的单测与 e2e——bash / zsh 读 list 失败（删除 state
  文件后直接跑生成的 stage 脚本）；四方言未赋值标量；python / go 预检的
  缺失与存在两条路径；全量质量闸门通过。
- **完成标准**：list 读失败不再产生空数组；未赋值标量在四方言下报同一句可读
  错误；缺 `python3` / `go` 时 `check` / `build` / `run` 均 fail-fast；既有
  示例（pipeline / primes / mixed-shells）行为不变。

## M21 — 持续集成

- **交付物**：
  - `.github/workflows/ci.yml`：push 与 pull_request 触发，两个 job——
    go（`gofmt -l .` 有输出即失败、`go vet`、`go test`、`go test -race`、
    提交自身的空白检查 `git show --check`）与 markdownlint（`npm ci` +
    `npm run lint:md`）。CI 额外安装 zsh 与 tcsh，让 shell 方言 e2e 真跑。
  - markdownlint 版本在仓库内锁定（`package.json` + `package-lock.json`，
    devDependencies 精确版本），使该闸门可复现且不依赖开发者本机装包。
  - 仓库根 `.gitignore` 增加 `node_modules/` 与 `tmp/`（后者是 AGENTS.md
    约定的临时目录）。
- **依赖**：M20（CI 校验 M20 的产物）。
- **验证**：本地逐条执行 workflow 中的命令（markdownlint 用仓库锁定的
  版本跑，`npm run lint:md`）；workflow YAML 用 PyYAML 解析自检。
- **完成标准**：CI 覆盖现有全部闸门，本地等价命令全过；不再有「闸门依赖人工
  记得执行」的项。

## 里程碑依赖

```text
M20 → M21
```

## 范围外（本阶段不做）

- 复合类型（map / struct / 嵌套列表）：架构 §1 列为非目标。
- 并行执行：架构 §6 明确推迟。
- csh 的一维数组支持：探针结论不可靠。
- 多文件 import 与插件机制：架构 §1 列为非目标。
