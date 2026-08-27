# Macaronic

macaronic 是一个类编译的 **CLI 构建工具**：把单个混用多种编程
语言的脚本文件（`.mac`）切分为各语言独立脚本，按头部契约表
**自动注入跨块变量的读写代码**，生成 shell 驱动按序调用各子程序。

## 特性

- 块标记 `#!lang`，head 块 `#!mac` 声明跨块变量契约（TOML
  `[contract]`）
- 基本类型：`int` / `float` / `bool` / `str`
- 首批块语言：`shell` + `python` + `go`
- 每变量一个二进制 state 文件（脚本内自洽 ABI，见
  `docs/architecture.md` §10）
- 顺序执行；失败即停并保留现场（`failure.json`）
- 语言无关引擎接口，内置注册表

## 安装

```sh
go build -o /usr/local/bin/macaronic ./cmd/macaronic
```

依赖：Go 工具链 ≥ 1.22；运行 `#!shell` 需 Bash、`#!python` 块需
Python 3、`#!go` 块需相同 Go 工具链。macaronic 自身需在 `PATH`
中（生成的脚本通过 `macaronic codec` 读写状态文件）。

## 快速开始

```sh
macaronic check hello.mac   # 静态检查
macaronic build hello.mac   # 生成产物目录，保留 state/
macaronic run  hello.mac    # 编译、清空 state、按序执行
macaronic       hello.mac   # 等价于 run
```

产物布局（`hello.mac.run/`）：每个块一个 `stageN/`，共享
`state/`，另含 `run.sh`、`sourcemap.json`、`failure.json`（失败时）。

```sh
macaronic codec read  state/count.macint int    # 调试辅助
macaronic codec write state/count.macint int 42
```

## 示例

见 [`examples/`](examples/README.md)：完整流水线示例与四个异常
路径用例（读未写、缺注解、遮蔽、运行时失败）。

## 文档

- [`docs/architecture.md`](docs/architecture.md)：规范性架构设计
- [`docs/development-plan.md`](docs/development-plan.md)：里程碑规划（阶段 3，M14–M16）
- [`docs/tasks.md`](docs/tasks.md)：详细任务清单与进度（阶段 3）
- [阶段 2 计划归档](docs/archive/development-plan-phase2.md)
- [阶段 2 任务归档](docs/archive/tasks-phase2.md)
- [阶段 1 计划归档](docs/archive/development-plan.md)
- [阶段 1 任务归档](docs/archive/tasks.md)
- [`docs/archive/tasks.md`](docs/archive/tasks.md)：阶段 1 任务清单与进度（已完成，已归档）

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
