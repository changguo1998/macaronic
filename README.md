# Macaronic

macaronic 是一个类编译的 **CLI 构建工具**：把单个混用多种编程
语言的脚本文件（`.mac`）切分为各语言独立脚本，按头部契约表
**自动注入跨块变量的读写代码**，生成 shell 驱动按序调用各子程序。

## 特性

- 块标记 `#!lang`，head 块 `#!mac` 声明跨块变量契约（TOML
  `[contract]`）
- 基本类型：`int` / `float` / `bool` / `str`；支持一维数组
  `int[]` / `float[]` / `bool[]` / `str[]`
  `string[]` 是兼容别名，规范化为 `str[]`
- 块语言：shell 方言 `bash` / `sh` / `zsh` / `csh`，另加 `python` / `go`
  （能力矩阵见 `docs/architecture.md` §1；`sh` 与 `csh` 不支持一维数组，
  引用数组类型的契约变量会在 check 阶段报错）
- 每变量一个二进制 state 文件（脚本内自洽 ABI，见
  `docs/architecture.md` §10）
- 顺序执行；失败即停并保留现场（`failure.json`）
- 语言无关引擎接口，内置注册表

## 安装

```sh
go build -o /usr/local/bin/macaronic ./cmd/macaronic
```

依赖：Go 工具链 ≥ 1.22；`#!bash` 需 Bash、`#!sh` 需 POSIX sh、
`#!zsh` 需 zsh、`#!csh` 需 **tcsh**（失败即停依赖其 `-e`）、`#!python`
块需 Python 3、`#!go` 块需相同 Go 工具链。macaronic 自身需在 `PATH`
中（生成的脚本通过 `macaronic codec` 读写状态文件）。

缺失上述解释器时 `check` / `build` / `run` 会 fail-fast 报错（含方言名
与所查命令），不会出现「check 通过但实际跑不了」。

## 快速开始

```sh
macaronic parse hello.mac   # 解析并打印块列表与契约
macaronic check hello.mac   # 静态检查
macaronic build hello.mac   # 生成产物目录，保留 state/
macaronic run  hello.mac    # 编译、清空 state、按序执行
macaronic       hello.mac   # 等价于 run
```

四个子命令就是流水线的四步（`parse → check → build → run`）：每个命令
跑完自己那一步就停，`run` 是整条流水线加执行。

产物布局（`hello.mac.run/`）：每个块一个 `stageN/`，共享
`state/`，另含 `run.sh`、`sourcemap.json`、`failure.json`（失败时）。

```sh
macaronic codec read  state/count.macint int    # 调试辅助
macaronic codec write state/count.macint int 42
macaronic codec read-list state/values.macint[] int[]
macaronic codec write-list state/values.macint[] int[] 1 2 3
```

## 示例

见 [`examples/`](examples/README.md)：完整流水线示例与四个异常
路径用例（读未写、缺注解、遮蔽、运行时失败）。

## 文档

- [`docs/architecture.md`](docs/architecture.md)：规范性架构设计
- 阶段 4 归档：[计划](docs/archive/development-plan-phase4.md) /
  [任务](docs/archive/tasks-phase4.md)（M17–M19 已完成，阶段 5 待规划）
- 阶段 3 归档：[计划](docs/archive/development-plan-phase3.md) /
  [任务](docs/archive/tasks-phase3.md)
- 阶段 2 归档：[计划](docs/archive/development-plan-phase2.md) /
  [任务](docs/archive/tasks-phase2.md)
- 阶段 1 归档：[计划](docs/archive/development-plan.md) /
  [任务](docs/archive/tasks.md)

## 开发

```sh
go test ./...            # 单元测试
go test -race ./...      # 竞态检测
gofmt -l .               # 格式
npx --no-install markdownlint-cli2 docs/ examples/  # markdown 检查
```

语言引擎实现 `internal/engine` 下的 `engine.Engine` 接口并用
`engine.Register` 注册（见 `cmd/macaronic/main.go`）。

## 许可

MIT
