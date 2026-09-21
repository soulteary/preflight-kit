# preflight-kit

[![CI](https://github.com/soulteary/preflight-kit/actions/workflows/ci.yml/badge.svg)](https://github.com/soulteary/preflight-kit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/preflight-kit.svg)](https://pkg.go.dev/github.com/soulteary/preflight-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

启动自检：不只说哪里不对，还说照着做什么。零依赖。

**文档:** [English](README.md) · 中文

## 问题出在时机

大多数部署失误 —— 进程写不了的目录、已经不存在的网络、当前用户没权限的 socket —— 在进程启动的那一刻完全可以检出。

但它们往往要等到很久以后，由第一个真正需要它们的任务发现。到那时，故障以**那个任务**恰好失败的方式呈现，发生在没人盯着的时刻，错误信息里也完全看不出是哪个配置项引起的。

```go
results := preflight.Run(ctx,
    preflight.Named("数据目录", func(ctx context.Context) preflight.Result {
        return preflight.DirWritable("数据目录", cfg.DataDir)
    }),
    preflight.Named("docker socket", func(ctx context.Context) preflight.Result {
        return preflight.SocketAccessible("docker socket", "/var/run/docker.sock")
    }),
)
results.Log(log.Printf)
```

## 安装

```bash
go get github.com/soulteary/preflight-kit
```

包名是 `preflight` 而不是 `preflight-kit`，所以 import 时把名字写出来：

```go
import preflight "github.com/soulteary/preflight-kit"
```

## 只报告问题而不给做法，等于把活儿挪走了而不是干完了

`Result` 除了 `Message` 还带 `Hint`，并且 message 应当写出**真实的**路径、地址或取值。

> `/srv/runners 对 uid 1001 不可写` 能把读的人送到某个地方。
> `权限有问题` 不能。

## 本包坚持的三条

**检查是只读的。** 自检在每次启动时都跑，包括那些本来就已经不顺利的启动。一个带副作用的自检，只会多一个把糟糕情况变得更糟的东西。

**检查失败绝不阻止程序启动。** 报告就是它的全部工作。一个会拦住启动的自检，在它第一次对作者没预料到的环境判断错误时，就会被删掉。

**通过的结果也要记日志。**`docker daemon 27.0.3、网络 app-net 存在、/srv/data 对 uid 1001 可写` 是这套环境当时长什么样的记录 —— 三周后同一个部署出怪毛病时，人们最先看的就是它。

## 内置探针

| 探针 | 实际检查的东西 |
|---|---|
| `DirWritable` | 能建文件**并且能删掉**。见下 |
| `SocketAccessible` | socket 存在**并且**本进程在它的属组里 |
| `TCPReachable` | 真的有人应答 —— 且永远带超时 |
| `SubdirsPrivate` | 哪些目录能被其他用户进入。只报告，从不修改 |
| `PathExists`、`CommandsPresent`、`FileGID`、`InGroup` | |

### 为什么 `DirWritable` 还要删一次

一个目录可能允许你新建文件、却不允许你删掉它 —— sticky bit，或者一写就变只读的挂载。停在「建出来了」的检查会说这个目录没问题。程序于是在更晚的时候失败：在第一个需要替换或清理文件的操作上，而且和这个配置项之间已经没有任何可追溯的联系。

探测文件用随机名，因为固定名会和同名的真实文件相撞 —— 一个会打开并删除用户数据的自检，比没有自检更糟。

### 为什么 `TCPReachable` 永远带超时

一个把包吞掉的不可达地址会一直挂到 TCP 协议栈放弃为止，这远远超过任何人愿意在启动时为一个「本该让人安心」的检查所等待的时间。超时传 0 时取 `DefaultDialTimeout`，而不是「永远」。

### 为什么 `SubdirsPrivate` 只报告

收紧权限可能弄坏一个 uid 并不按检查者设想那样对齐的部署。这个决定属于能看到全局的人；检查的职责是确保他们知情。

## 诊断事后发生的故障

preflight 回答「这套部署是否健全」。`Classifier` 回答另一半：运行期出故障时，它属于哪一类、该怎么办。

```go
var classifier = preflight.Classifier{
    Rules: []preflight.Rule{{
        Kind:       "docker-access",
        Substrings: []string{"permission denied", "cannot connect to the docker daemon"},
        Advice: preflight.Advice{
            Suggestion:   "检查 socket 挂载与属组",
            CheckCommand: "ls -l /var/run/docker.sock && id",
            FixCommand:   "usermod -aG docker $USER && systemctl restart docker",
        },
    }},
    Fallback: preflight.Advice{CheckCommand: "docker compose ps"},
}

d := classifier.Diagnose(err)   // Kind, Error, Suggestion, CheckCommand, FixCommand
```

**`CheckCommand` 与 `FixCommand` 刻意分开。** 在别人的机器上排查问题的人，需要能先看后动；而只有一个「执行这条」的字段，会逼作者在「给一条安全的命令」和「给一条有用的命令」之间二选一。分开之后，只读的那条总能先给出来，界面也能把另一条呈现为「操作」而不是「信息」。

**在故障发生处给错误打标签，文本匹配只作兜底。**

```go
return preflight.Wrap("agent-connect", err)
```

在故障发生处打上的标签，知道自己是什么。按错误文本匹配是一种会**静默出错**的猜测 —— 它能一直活到某人改了一句措辞、或某个库升级为止，然后悄无声息地把所有东西都归到兜底那一类。`Diagnose` 先看标签、再退回子串匹配，于是既有调用点照常工作，新的可以逐步打上标签。

## 结果集

```go
results.OK()             // 没有 error；warning 不算
results.Worst()          // LevelOK | LevelWarn | LevelError
results.Problems()       // warning 与 error，保持检查顺序
results.Log(log.Printf)  // 每条结果一行
results.SortedByLevel()  // 最严重的排前面，给界面用
```

`Run` **按顺序、串行**执行检查。检查本身很便宜，而书写顺序通常就是最好读的顺序 ——「目录在不在」先于「镜像在不在」先于「Job 能不能连上 docker」。并发执行省下几毫秒，代价是把人们真正要读的那个东西打乱。

panic 的检查会变成一条 error 结果，而不是把整个程序带走。

## 要求

- **Go 1.22+**（`go.mod` 中声明 `go 1.22.0`）。本包没有任何地方需要更新的工具链，
  而库的 `go` 指令对所有导入方都是硬性下限，所以压到代码允许的最低。CI 会同时
  用 1.22 和当前发行版跑测试。
- **零依赖。** 连测试在内，全部只用标准库。
- **仅限 Unix。** `FileGID` 通过 `syscall.Stat_t` 读取 POSIX 属主信息，因此本包
  在 Windows 上无法编译。macOS 与 Linux 都可以；不过探针描述的那套语义 ——
  uid、gid、socket 属主 —— 是 Linux 的。

## 测试覆盖率

```bash
go test ./... -v

# 带覆盖率 —— CI 实际执行的命令
go test -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
go tool cover -func=coverage.out
```

语句覆盖率为 **94.9%**。测试任务会在 Linux 和 macOS 上、用 Go 1.22 和当前发行版
各跑一遍；其中一个组合会把可浏览的 HTML 报告作为构建产物上传。不接入任何覆盖率
服务。

`example_test.go` 里的可运行示例是测试套件的一部分。它们是*外部*测试包
（`package preflight_test`），只能编译到导出的 API —— 这能逼着这套 API 对包外
调用者保持可用 —— 而且 `go test` 会校验它们打印的输出，因此示例不会与文档
所述发生偏移。

## 变更日志

见 [CHANGELOG.md](CHANGELOG.md)。

## 安全

结果里有意写出真实的路径、uid 和地址，这让输出成为给运维看的日志，而不是可以
对外公开的东西。hint 是命令，但本包不会执行它们。[SECURITY.md](SECURITY.md)
解释了这两点对调用方意味着什么，以及如何上报安全问题 —— 请不要为安全问题开
公开 issue。

## 贡献

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

## 许可证

Apache 2.0，见 [LICENSE](LICENSE)。
