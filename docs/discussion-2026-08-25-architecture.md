# Macaronic 架构讨论记录（2026-08-25）

> 状态：顶层架构已收敛，留有两条继续线。可随时继续讨论。

## 结论一：本项目不是执行器，是构建工具

输入混合语言脚本 → 切分为各语言脚本 → 生成 shell 驱动按期调用各子程序。

```
foo.mac（混合源文件）
   │ parse：按块标记切分 → [{lang, code}, ...]
   ▼
emit：每块落成独立文件 + 生成驱动脚本
   ├── build/stage1.py     (python 块)
   ├── build/stage2.sh     (bash 块)
   └── build/run.sh        (驱动：顺序调用 + 管道衔接)
```

Bridge 复杂度归零，驱动本身是 shell，衔接格式天然是 stdout→stdin。两层：切块、拼驱动。

## 结论二：同类工具调研

无确切同款；最接近的是 Org Babel（Emacs，代码块 + `:var` 传结果 + `:session` 会话）、
.NET Interactive（多语言变量共享，2026-04 已归档）、knitr（语言引擎注册表）、
Runme（可执行 Markdown，shell 中心）、marcel（仅 shell+Python 混合交互 shell）。

差异化定位：**脚本文件（非文档/notebook）+ 命令行构建（非交互）+ 块间自动衔接（非手动 export）**。
可借鉴：knitr engine 注册表、org-babel :var 显式传参、.NET Interactive 值级共享。

## 结论三：管道局限与替代衔接方式

局限：单一字节流、无结构化、纯文本降级、不可断点重跑、不可分支、错误边界模糊、无法反向通信。

替代（按复杂度升序）：中间文件、环境变量、命令参数、命名管道 FIFO、共享 JSON 文件、unix socket 消息、数据库/消息队列、FFI 嵌入/常驻解释器。

结论：MVP 用管道；第一步升级 JSON 文件/共享状态；session/kernel 级别不自研，真有需求做 .NET Interactive 格式兼容。突破点的关键是去掉"线性"或"匿名"。

## 结论四：可衔接的数据类型

分两档：

### 直接字节交换（POD，约定布局）

- int/u32/i64
- float/double (IEEE 754)
- bool (0/1)
- char/byte/enum(底层宽度)
- 定长数组 (numpy ndarray 即 C array 布局)
- C struct（仅 POD 成员，同 ABI 内）

### 需要序列化

- string/字节数组（长度前缀）
- map/hash/dict
- 树/图/链表
- 对象/多态（类型 tag）
- 资源句柄

注意：需要用户偏好最常用 **int/float/bool/定长数组/string(长度前缀)/定长 struct(同 ABI)**——完备。序列化可以传任何类型，但 JSON 等格式传的是「值在格式词汇表里的投影」；函数/身份/资源句柄永远传不了。要传任意类型需 schema/IDL + 代码生成（protobuf/flatbuffers）。

## 结论五：防止滑向「发明新语言」

边界在「谁定义计算」：

| 层 | 性质 |
|---|---|
| 块切分标注 | 格式约定，非语言 |
| 驱动生成 | Makefile 同类，非语言 |
| 数据契约 schema | 接口定义，非语言 |

危险区：块级控制流（if/loop）、自研类型语义、跨语言对象行为定义——只要有一层进入运行语义就滑向新语言。
防滑线：**类型由双方各自的真语言定义，macaronic 只传送字节，不判语义**；控制流留在 shell 驱动里。

## 继续线

1. 定型块标记语法（初步：`#!python` 块标注，`#!go` 编译型语言先 build 再跑）
1. 产物目录布局（初步：`foo.mac` → `build/foo/stageN.ext` + `run.sh`）
1. 实现语言（建议 Python 起步）
1. 序列化层选型（CBOR/msgpack vs protobuf，轻量自描述 vs schema 代码生成）
