# Macaronic 里程碑任务清单（阶段 2）

> 本文件是根据 [`docs/development-plan.md`](development-plan.md)
> （阶段 2）拆解的执行级任务清单，每个里程碑定义、依赖、交付物、
> 验证原则均以开发计划为准。阶段 1（M1–M10）任务清单归档于
> `archive/tasks.md`。每个任务（T-ID）粒度约 0.5–2 工时。

## 任务维护约定

- 每个 T-ID 对应一个具体的实现/测试动作，完成后把 `[ ]` 改为 `[x]`。
- 每个 T 完成的标准是它自带的「验收」句；里程碑完成的标准是该节
  T 全部打勾。
- 开发计划中的**固定质量闸门**在每个里程碑都需通过：
  1. `gofmt -l .` 无输出
  2. `go vet ./...` 无输出
  3. `go test ./...` 全绿
  4. M13 额外加 `go test -race ./...` 全绿

## 里程碑依赖（同 development-plan.md）

```text
M10（阶段 1）→ M11 → M12 → M13
```

## 进度总览

| 里程碑 | 预估工作量 | 已打勾 / 总数 |
| --- | --- | --- |
| M11 | 4–6 工时 | 5 / 5 |
| M12 | 3–4 工时 | 4 / 4 |
| M13 | 6–8 工时 | 8 / 8 |
| **M11–M13** | **约 13–18 工时** | **17 / 17** |

---

## M11 — 警告体系与既有缺口补齐

- [x] T11.1 `internal/analyze`：`Issue` 增加 `Severity` 字段
  （error/warning，零值为 error）；报告行前缀 `error:` /
  `warning:`；`issuePrefix` 在 Stage==0 时不打印块号。
  验收：`go test ./internal/analyze/` golden 更新通过。
- [x] T11.2 `Report.OK()` 重定义为「无 error」（warning 不阻断）；
  `check`/`build` 调用点保持基于 OK。验收：error fixture 行为
  不变（退出非零）。
- [x] T11.3 程序级 unused 告警：契约变量不在任何块推断读/写集
  合、且不在任何块源码词法出现集合（inferred ∪ observed）→
  warning（Stage=0）。验收：table-driven（未用告警、仅 observed
  不误报）。
- [x] T11.4 CLI fixtures 与测试：warning-only `.mac` 的 `check`
  退出 0 且输出含 warning 行；`build` 同样退出 0；error fixture
  （读未写）退出非零。验收：`go test ./internal/cli/`。
- [x] T11.5 文档：阶段 2 计划/任务文档就位，README 导航更新
  （阶段 2 现行链接 + 阶段 1 归档链接）。验收：markdownlint 通过。

## M12 — 「引用但未推断」检测

- [x] T12.1 框架对每块对契约名做 token 级兜底扫描：observed 不在
  inferred → warning「读可能未注入，请人工确认」。验收：
  table-driven。
- [x] T12.2 引擎 Analyze 对该块返回 error 时抑制该块 M12 warning；
  词法出现仍计入 observed。验收：table-driven（遮蔽/缺注解块不
  重复上报）。
- [x] T12.3 与 unused 告警去重：仅 observed（未被推断）的变量不
  发 unused warning。验收：table-driven。
- [x] T12.4 CLI fixture：shell 块名字仅出现在字符串（无 `$` 前缀）
  → M12 warning 且退出 0。验收：`go test ./internal/cli/`。
  备注：python 引擎把字符串内出现视为缺注解错误，无法构造
  纯 M12 warning，故 fixture 用 shell。

## M13 — 逐引擎推断增强（6 模式）

- [x] T13.1 python 括号续行：纯写判定的 RHS 引用检查跨越未闭合
  括号的逻辑行。验收：命名 golden 子测试通过。
- [x] T13.2 python 下标写 `v[...] = x` 记读 + 写。验收：命名
  golden 子测试通过。
- [x] T13.3 python `def f(v)` 参数遮蔽错误（先于缺注解错误上
  报）。验收：命名 golden 子测试通过。
- [x] T13.4 shell `read -r v` / `read v` 记写。验收：命名 golden
  子测试通过。
- [x] T13.5 shell `$((... v ...))` 算术引用记读。验收：命名
  golden 子测试通过。
- [x] T13.6 go `v++`/`v--` 读改写命名 golden 回归子测试（现有
  identOp 实现不变）。验收：子测试通过。
- [x] T13.7 `architecture.md` §12「推断失败则不注入」条目注明
  现已发 M12 warning。验收：markdownlint 通过。
- [x] T13.8 全量闸门：`go test ./...` + `go test -race ./...`
  全绿；现有 shell→python→go E2E 无回归。验收：全绿。

## 依赖约束

> 不新增依赖：改动限于 `internal/analyze`、
> `internal/engine/{python,shell,golang}`、`internal/cli` 与
> `docs/`、README；go.mod/go.sum 不变。
