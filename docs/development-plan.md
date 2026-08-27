# Macaronic 开发计划（阶段 2）

> 阶段 2 主题：**推断/诊断增强**。阶段 1（M1–M10，MVP）已归档于
> `archive/development-plan.md`。每个里程碑列交付物、依赖、验证命令与
> 完成标准。
> 测试约定：table-driven 单元测试 + golden tests + shell→python→go
> 端到端（E2E）。
> **固定质量闸门**（每个里程碑默认全跑）：`gofmt -l .`（无输出）、
> `go vet ./...`、`go test ./...`；涉及锁/并发/子进程的里程碑加
> `go test -race ./...`。下方「验证」只列里程碑专属检查。

## M11 — 警告体系与既有缺口补齐

- **交付物**：
  - `internal/analyze`：`Issue` 增加 `Severity` 字段（error/warning，
    零值为 error）；`Report.OK()` 重定义为「无 error」（warning 不
    阻断），`check`/`build` 调用点语义同步；报告行前缀
    `error:` / `warning:`，保持 (stage, line, var) 确定性排序；
    程序级问题（Stage=0）不打印块号。
  - 新 warning：契约变量未被任何块推断为读/写、且未在任何块源码
    中出现（inferred ∪ observed 双集合判定）→「declared but unused」
    （抓拼写错误）。
  - 阶段 2 计划/任务文档就位，README 文档导航更新（阶段 2 现行
    链接 + 阶段 1 归档链接）。
- **依赖**：M1–M10（阶段 1 完成）。
- **验证**：analyze table-driven（warning 不阻断 OK、unused 告警、
  Stage=0 输出形态）；CLI golden：warning-only fixture `check` 与
  `build` 均退出 0 且输出含 warning 行；error fixture（读未写）退出
  非零。
- **完成标准**：warning 可见但不阻断 check/build；error 路径行为
  不变；现有测试全绿。

## M12 — 「引用但未推断」检测

- **交付物**：框架对每块对契约名做 token 级兜底扫描（原始文本，
  含注释/字符串，轻量安全网）：名字出现在块源码（observed）但不在
  引擎推断出的读/写集合（inferred）→ warning「读可能未注入，请人工
  确认」。引擎 Analyze 对该块返回 error 时抑制该块的 M12 warning
  （避免与遮蔽/缺注解错误重复上报），词法出现仍计入 observed。
  与 unused 告警去重：仅 observed 的变量不发 unused。
- **依赖**：M11。
- **验证**：analyze table-driven（observed-not-inferred 告警、
  引擎出错抑制、与 unused 去重）；CLI fixture：python 块名字仅
  出现在字符串中 → warning 且退出 0。
- **完成标准**：架构 §12 已知限制「推断失败→不注入→运行时未定义
  名」前移为 check 期可见的 warning；现有三引擎已覆盖的场景不新增
  误报。

## M13 — 逐引擎推断增强（严格限定 6 模式）

- **交付物**（每模式一个命名 golden 测试）：
  1. **python 括号续行**：纯写判定的 RHS 引用检查跨越未闭合括号的
     逻辑行（`v = f(\n v)` 中 v 计读）；
  2. **python 下标写**：`v[...] = x` 记读 + 写（原地修改需要旧值，
     必须注入 prologue）；
  3. **python def 参数遮蔽**：`def f(v)` 契约名作函数参数记遮蔽
     错误（对齐 go `:=` 规则，先于缺注解错误上报）；
  4. **shell read 内建**：`read -r v` / `read v` 记写（read 是块的
     生产者）；
  5. **shell 算术引用**：`$((... v ...))` 记读（独立块
     `count=$((count+1))` 必须消费前块值，避免静默重置为 0）；
  6. **go 自增减**：`v++`/`v--` 读改写命名 golden 回归测试
     （现有 identOp 实现已正确，不改代码）。
- **依赖**：M11、M12。
- **验证**：6 个命名子测试全部通过（`go test ./internal/engine/...
  -v`）；现有 shell/python/go E2E 无回归；`go test -race ./...`
  全绿。
- **完成标准**：6 模式推断结果正确且 Emit 的 prologue/epilogue 随之
  变化（新写进 epilogue、新读进 prologue）；`architecture.md` §12
  「推断失败则不注入」条目注明现已发 M12 warning。

## 里程碑依赖

```text
M10（阶段 1）→ M11 → M12 → M13
```
