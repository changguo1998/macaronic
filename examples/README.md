# Macaronic 多语言脚本示例

本目录包含可运行的 `.mac` 示例。每个示例是一个文件，
`macaronic run` 一键执行。运行前确保 `macaronic` 在 `PATH` 中。

## 完整示例（bash → python → go）

见 `pipeline.mac`（主示例）：bash 生成数据，python 加工，go
汇总输出，四类基本类型（int/float/bool/str）全部传递。

```sh
# 构建 macaronic（若未安装）
go build -o /tmp/macaronic ./cmd/macaronic

# 运行示例（需 macaronic 在 PATH 中）
export PATH=/tmp:$PATH
macaronic run pipeline.mac
```

预期输出：

```text
pipeline.mac: running 3 stage(s)
final values: count=41 total=2.5 ok=true msg=hello from bash & python
pipeline.mac: ok
```

## 质数筛（python → go，int[] 数组）

`primes.mac`：python 用埃氏筛计算 1000 以内全部质数，写入 `count`
（int）与 `primes`（int[]）两个契约变量，go 汇总求和并输出，
演示一维数组 `int[]` 跨块传递。

```sh
macaronic run primes.mac
```

预期输出：

```text
primes.mac: running 2 stage(s)
primes under 1000: count=168 sum=76127 first=2 last=997
primes.mac: ok
```

## 四方言 shell 混合（bash → sh → zsh → csh）

`mixed-shells.mac`：同一份契约数据在四个 shell 方言间流转——bash 建初值，
sh 与 zsh 做标量加工（zsh 另追加数组元素），csh 做算术与字符串拼接，最后
bash 汇总输出。

注意 `sh` 与 `csh` 不支持一维数组契约类型（引用即 check 报错），所以数组
只出现在 bash / zsh 阶段；`csh` 需 **tcsh**，因为失败即停依赖其 `-e`。

```sh
macaronic run mixed-shells.mac
```

预期输出：

```text
mixed-shells.mac: running 5 stage(s)
final: count=30 msg=[bash seed | sh +5 | zsh | csh] squares=[1 4 9 16]
mixed-shells.mac: ok
```

缺任一方言解释器时 `check` / `build` / `run` 都会 fail-fast 报错，而不是
等到运行时才失败。

## 异常路径用例

- `read-before-write.mac` — 未写先读：`macaronic check` 应报错。
- `missing-annotation.mac` — python 块契约变量缺注解：check 报错。
- `shadow.mac` — go 块用 `:=` 新建与契约变量同名的绑定：check
  （Emit）报错。
- `runtime-failure.mac` — python 运行时异常：run 非零退出并在
  `failure.json` 留下现场。

```sh
macaronic check read-before-write.mac   # exit 1
macaronic check missing-annotation.mac  # exit 1
macaronic check shadow.mac              # exit 1
macaronic run runtime-failure.mac       # exit 1，产出 failure.json
```
