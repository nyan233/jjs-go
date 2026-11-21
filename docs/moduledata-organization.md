# `moduledata` 的组织方式

本文解释 Go runtime 如何使用 `moduledata` 管理一个可执行模块，以及
`jjs-go/internal/jitloader` 如何为动态生成的单函数模块构造这些数据。

主要源码依据：

- Go 1.21.13 [`runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.21.13/src/runtime/symtab.go)
- Go 1.23.12 [`runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.23.12/src/runtime/symtab.go)
- Go 1.24.0 [`runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.24.0/src/runtime/symtab.go)
- Go 1.26.11 [`runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.26.11/src/runtime/symtab.go)
- Go 1.27.4 [`runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.27.4/src/runtime/symtab.go)
- Go 1.27.4 [`internal/abi/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.27.4/src/internal/abi/symtab.go)

## 1. `moduledata` 是什么

`moduledata` 是 runtime 对“一个包含机器码和 Go 符号信息的模块”的描述。
普通 Go 程序的 `.text`、`.rodata`、pclntab、类型信息、初始化任务等由 linker
写入一个或多个 `moduledata`。`plugin` 和 `buildmode=shared` 会动态追加新的
`moduledata`。

runtime 通过单向链保存这些模块：

```text
runtime.firstmoduledata
          |
          v
     moduledata A
        .next
          |
          v
     moduledata B
        .next
          |
          v
        nil
```

JIT loader 加载新代码时，也必须把新模块追加到这条链中。否则 stack
unwinding、GC stack scan、`runtime.Callers`、pprof 和 panic traceback 都可能
在 JIT 帧处停止或解释错误的栈布局。

## 2. 字段分组

`moduledata` 字段很多，但可以按职责分为以下几组。

```text
moduledata
├── pclntab / function discovery
│   ├── pcHeader
│   ├── funcnametab
│   ├── cutab
│   ├── filetab
│   ├── pctab
│   ├── pclntable
│   ├── ftab
│   ├── findfunctab
│   └── minpc, maxpc
├── executable memory
│   ├── text, etext
│   └── textsectmap
├── global data and GC
│   ├── data, edata
│   ├── bss, ebss
│   ├── noptrdata, noptrbss
│   ├── gcdata, gcbss
│   └── gcdatamask, gcbssmask
├── function data
│   ├── gofunc
│   └── epclntab (Go 1.26+)
├── runtime type metadata
│   ├── types, etypes
│   ├── typelinks / itablinks (Go 1.21–1.26)
│   └── typedesclen / itaboffset / itabsize (Go 1.27+)
├── module identity
│   ├── modulename
│   ├── modulehashes
│   └── pluginpath
└── module chain
    └── next
```

### 2.1 PC 与函数查找

核心入口是 `pcHeader`。它记录 pclntab 版本、指针宽度、指令对齐，以及后续各
table 在 `moduledata` 中的字段偏移。

```text
          pcHeader
             |
             +-- funcname -> moduledata.funcnametab
             +-- cu       -> moduledata.cutab
             +-- filetab  -> moduledata.filetab
             +-- pctab    -> moduledata.pctab
             +-- pcln     -> moduledata.pclntable
```

Go 1.20 起 pclntab magic 为 `0xfffffff1`，Go 1.21 到 1.27 都继续使用这个值。
当前 loader 的 `pcHeader` 位于
[`module_common.go`](../internal/jitloader/module_common.go)。

### 2.2 `ftab` 和 `findfunctab`

`ftab` 是按 PC 排序的函数表：

```go
type functab struct {
    entryOff uint32
    funcOff  uint32
}
```

`entryOff` 是相对 `moduledata.text` 的代码偏移，不是绝对地址。`funcOff` 是
`_func` 在 `moduledata.pclntable` 内的偏移。

```text
text base
   |
   v
 [ generated code ][ generated code ][ ... ]
     ^                    ^
     | ftab[0].entryOff   | ftab[1].entryOff
     |                    |
 ftab[0].funcOff      ftab[1].funcOff
     |                    |
     v                    v
 pclntable[_func 0]   pclntable[_func 1]
```

`ftab` 最后需要一个只包含结束 PC 的哨兵项：

```text
ftab = [
    {entryOff: functionStart, funcOff: functionInfoOffset},
    {entryOff: textEnd,                   funcOff: 0}
]
```

`findfunctab` 把 text 划分成 4 KiB bucket 和 256 B subbucket，先快速缩小候选
函数范围，再在 `ftab` 内查找。本项目每次只加载一个函数，因此所有 subbucket
都指向 `ftab[0]`。

### 2.3 `_func`

`_func` 描述单个函数。固定头之后紧跟两个变长数组：

```text
_func
├── entryOff
├── nameOff
├── args
├── pcsp
├── pcfile
├── pcln
├── npcdata
├── cuOffset
├── startLine
├── funcID / flag
├── nfuncdata
├── pcdata[npcdata]uint32
└── funcdata[nfuncdata]uint32
```

PC 相关字段指向 `moduledata.pctab`：

```text
_func.pcsp   -> pctab segment: PC -> stack pointer delta
_func.pcfile -> pctab segment: PC -> source file index
_func.pcln   -> pctab segment: PC -> source line
```

`pcdata` 数组负责运行时控制信息：

| Index | 用途 |
| ---: | --- |
| 0 | `PCDATA_UnsafePoint` |
| 1 | `PCDATA_StackMapIndex` |
| 2 | `PCDATA_InlTreeIndex` |
| 3 | `PCDATA_ArgLiveIndex` |

JIT 代码禁止异步抢占，因此 index 0 的值使用
`PCDATA_UnsafePointUnsafe`；index 1 固定选择本模块唯一的 StackMap。

### 2.4 `pcdata` 的编码

`pctab` 不是普通数组，而是增量编码的 `(valueDelta, pcDelta)` 序列：

```text
valueDelta = currentValue - previousValue
pcDelta    = (currentPC - previousPC) / PCQuantum
```

`PCQuantum` 在 amd64 为 1，在 arm64 为 4。序列遇到 0 结束。

当前实现位于
[`module_build.go`](../internal/jitloader/module_build.go) 的
`encodePCData`。

### 2.5 `funcdata` 和 StackMap

`_func.funcdata` 不是“函数数据在其后”的普通偏移，而是相对
`moduledata.gofunc` 的偏移。index 0 和 1 分别是：

```text
FUNCDATA_ArgsPointerMaps   -> 参数指针位图
FUNCDATA_LocalsPointerMaps -> 局部变量指针位图
```

单 bitmap StackMap 的内存布局：

```go
type stackmap struct {
    n        int32   // bitmap 数量，本项目为 1
    nbit     int32   // 每个 bitmap 的 bit 数
    byteData [1]byte // 指针 bit，每 bit 对应一个 uintptr 槽
}
```

`1` 表示该栈槽包含指针，GC 在 helper 调用期间必须扫描它。

```text
gofunc
  |
  +-- offset 0:  ArgsPointerMaps stackmap
  +-- offset N:  LocalsPointerMaps stackmap

_func.funcdata[0] = 0
_func.funcdata[1] = N
```

### 2.6 代码地址和模块范围

```text
text  = 可执行代码映射的起始地址
etext = 映射结束地址
minpc = 第一个函数的地址
maxpc = 最后一个函数的结束地址
```

`text` 和 `etext` 按页映射，因此 `etext` 可能大于 `maxpc`。但
`moduledataverify1` 会检查 `maxpc == textAddr(ftab[last].entryOff)`。

### 2.7 类型、itab 和插件信息

普通 linker 模块可能包含大量运行时类型和 itab。JIT 模块没有额外 Go 包，
因此本项目把这些字段留空。

Go 1.27 修改了这部分布局：

```go
types, typedesclen, etypes uintptr
itaboffset, itabsize       uintptr
```

旧版单独出现的 `typelinks` 和 `itablinks` 字段被移除。不能因为 JIT 不使用
这些数据就从结构体中删除字段；字段缺失会改变后续 `next`、GC mask 等字段的
偏移，导致 runtime 读写错位。

### 2.8 GC 数据

`data`、`bss` 和 `gcdata` 描述模块全局变量区域。JIT 模块没有全局变量，但仍
必须设置非空 `gcdata`/`gcbss`。loader 使用一个共享空字节，并把
`gcdatamask`、`gcbssmask` 留为零值。

真正需要 GC 追踪的是 JIT 栈帧。它由 `_func` 的 pcdata 和 funcdata
StackMap 描述，而不是模块全局 GC mask。

### 2.9 模块身份和初始化

`modulename`、`pluginpath`、`modulehashes` 用于模块身份和 ABI 校验。
`inittasks` 用于动态加载模块的初始化任务；JIT 模块没有 init task，因此该
slice 为空。

`hasmain` 为 0，`bad` 为 false。

## 3. 本项目如何构造单函数模块

加载流程位于
[`module_build.go`](../internal/jitloader/module_build.go)：

```text
LoadOne
  |
  +-- mmapExecutable            分配 RW，复制机器码，切换 RX
  +-- build funcnametab         JIT 函数名
  +-- build cutab/filetab       一个虚拟源文件
  +-- build pctab               pcsp / unsafe point / stackmap index
  +-- build funcdata            args/local pointer maps
  +-- build _func               pcdata 和 funcdata offset 数组
  +-- build ftab                一个函数 + 结束哨兵
  +-- build findfunctab         单函数候选表
  +-- fill moduledata           text、tables、gofunc、GC 和范围字段
  +-- registerModule           追加到 runtime module chain
```

`ftab` 的实际形态是：

```text
ftab[0] = {entryOff: 0, funcOff: 0}
ftab[1] = {entryOff: uint32(len(code)), funcOff: 0}
```

`_func.entryOff` 为 0，函数机器码放在新映射起点。

## 4. 版本差异

| Go | `moduledata` 变化 | JIT loader 处理 |
| --- | --- | --- |
| 1.21–1.22 | Go 1.20 module 布局，含 `covctrs` | `module_go121.go` |
| 1.23–1.25 | `bad` 移到 GC mask 前 | `module_go123.go` |
| 1.24–1.25 | linkname 检查收紧 | 汇编调用 `runtime.addmoduledata` |
| 1.26 | 增加 `epclntab` | `module_go126.go` |
| 1.27 | 增加 `typedesclen`、`itaboffset/itabsize`，移除旧 typelinks/itablinks | `module_go127.go` |

源码文件：

- [`module_go121.go`](../internal/jitloader/module_go121.go)
- [`module_go123.go`](../internal/jitloader/module_go123.go)
- [`module_go126.go`](../internal/jitloader/module_go126.go)
- [`module_go127.go`](../internal/jitloader/module_go127.go)

## 5. 模块注册

Go 1.21–1.23 与 1.26+ 使用 `runtime.lastmoduledatap`：

```text
lastmoduledatap -> 新模块
旧 tail.next     -> 新模块
新模块.next      -> nil
```

Go 1.24–1.25 的 linker 不允许普通包直接 linkname 私有
`lastmoduledatap`。loader 使用 amd64/arm64 汇编把新模块指针放到平台约定寄存器，
然后调用 runtime 自己的 `addmoduledata`。这段路径位于
[`register_asm.go`](../internal/jitloader/register_asm.go) 和
`link_amd64.s` / `link_arm64.s`。

Go 1.26 起 runtime 再次显式标记 module chain linkname，因此恢复
[`register_link.go`](../internal/jitloader/register_link.go) 路径。

## 6. 关键不变量

1. `moduledata` 字段前缀必须与目标 Go 版本完全一致，即使字段不使用也不能删。
2. `ftab.entryOff` 是 text 偏移；`_func.entryOff` 也是 text 偏移。
3. `ftab` 必须按 PC 排序，最后必须有 text-end 哨兵。
4. `pclntable` 中的 `_func` 必须能被 `ftab.funcOff` 精确定位。
5. `gofunc + funcdataOffset` 必须直接指向 StackMap 字节。
6. StackMap 的 bit 与实际 `uintptr` 槽必须一一对应，否则 GC 会漏扫或误扫。
7. `minpc`、`maxpc`、`text`、`etext` 和 `ftab` 端点必须互相一致。
8. 所有 metadata slice 必须保持存活；loader 使用模块保留表阻止 GC 回收。
9. ARM64 机器码切换为 RX 后必须执行 I-cache invalidate。
10. JIT frame 必须声明为 unsafe point，避免 runtime 在任意 PC 异步抢占。

## 7. 与普通 linker 模块的区别

本项目使用的是 runtime 结构的一个受控子集：

- 一个模块只有一个函数。
- 没有全局变量、类型表、itab、plugin 或 init task。
- `ftab` 和 `findfunctab` 都只有最小条目。
- 所有函数数据由同一个 JIT loader 生成，而不是 linker。

结构字段不能因为“本项目不用”而删除，因为这些字段决定后续字段偏移；但可以
保持零值，并让真正需要的 table 指向合法最小数据。
