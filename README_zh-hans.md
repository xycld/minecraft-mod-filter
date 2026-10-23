<p align="center">
    <img src="./icon.png">
</p>

<h1 align="center">Minecraft mod filter</h1>
<p align="center"><a href="README.md">English</a> | 简体中文</p>

把它指向一个 mods 文件夹或整合包，它会告诉你每个模组是否需要装在**客户端**和**服务端**——开服时不用再靠猜，也能把服务端专属模组从客户端里清出去。

```
$ mcmodfilter -lang zh-CN ~/.minecraft/mods
模组                      客户端  服务端  可信度  主要依据
Sodium                    必需    不需要  高      @Mod 类 …/SodiumForgeMod 只在客户端加载
EMI                       必需    可选    中      Modrinth: client_only_server_optional
Create                    必需    必需    高      添加了方块/物品（644 个方块状态，785 个物品模型）
Architectury              必需    可选    中      前置库，只被服务端不需要的模组使用: OctoLib
...
应从专用服务器移除（21）:
  sodium-neoforge-0.6.13+mc1.21.1.jar
  ...
```

## 特点

- **支持所有加载器**：Forge（从 1.12 的 `mcmod.info` 到新版 `mods.toml`）、NeoForge、Fabric、Quilt，包括多加载器 jar 和内嵌在其它 jar 里的模组。
- **真正读取 jar 内容**，而不只是看名字：加载器元数据、`@Mod(dist = …)` / `clientSideOnly` 注解、mixin 配置及其目标类、哪些类用到了客户端代码、是否添加了方块/物品/世界生成。
- **精确匹配平台数据**：按文件哈希在 Modrinth 上匹配（不靠模糊的名字匹配）；提供 API key 后还会按指纹查询 CurseForge。
- **理解前置依赖**：服务端模组需要的前置库会保留在服务端；只有客户端模组在用的前置库会被标为服务端可选。
- **给出理由**：每个结论都有可信度和依据（`-explain` 查看全部）。
- **可离线使用**（`-offline`），只需要 jar 文件本身。
- 支持 mods 文件夹、单个 jar、Modrinth `.mrpack` 和 CurseForge 整合包 `.zip`。
- 表格、JSON、Markdown 三种输出；English 和简体中文。
- Windows、macOS、Linux 单文件可执行程序。绝不会运行被扫描 jar 里的代码。

## 准确率

在 707 个带有作者声明的真实模组 jar 上测量，只统计会造成实际问题的错误：删掉了某一端需要的模组，或保留了在那一端无法运行的模组。

| 测试集 | jar 数 | 离线正确率 |
|---|---|---|
| Fabric 1.20.1 / 1.21.1、Forge 1.12.2 / 1.20.1、NeoForge 1.21.1、一个真实 NeoForge 整合包 | 407 | 97.8% |
| 从未用于调参的版本：Fabric 1.19.2、Quilt 1.20.1、Forge 1.16.5 / 1.18.2、NeoForge 1.20.4 | 300 | 97.7% |

联网时还会叠加模组作者自己的声明。在 *Smallish Bits* NeoForge 整合包上，它判断应保留在服务端的模组与整合包作者官方服务端包一致的比例为 90.7%；差异来自可选模组，以及官方服务端包里多带的三个仅客户端模组。

## 安装

从 [Releases](https://github.com/xycld/minecraft-mod-filter/releases) 下载，或用 Go 1.26+ 编译：

    go install github.com/xycld/minecraft-mod-filter/cmd/mcmodfilter@latest

## 用法

    mcmodfilter [参数] <路径>

`<路径>` 可以是 mods 文件夹（或包含 `mods/` 的实例文件夹）、单个 `.jar`、`.mrpack` 或 CurseForge `.zip`。不填则扫描 `./mods`。

| 参数 | |
|---|---|
| `-format table\|json\|markdown` | 输出格式（默认 `table`） |
| `-explain` | 列出每个结论背后的全部依据 |
| `-offline` | 只分析 jar，不联网 |
| `-loader fabric\|quilt\|forge\|neoforge` | 多加载器 jar 以哪个加载器的元数据为准（默认自动检测） |
| `-curseforge-key KEY` | 启用 CurseForge 查询（也可设置 `CURSEFORGE_API_KEY`） |
| `-download` | 下载 `.mrpack`/CurseForge 整合包引用的文件并一起分析 |
| `-lang en\|zh-CN` | 输出语言（默认跟随 `LANG`） |
| `-no-cache` | 不使用 API 缓存（缓存保存在用户缓存目录，7 天有效） |

结果含义：

- **必需**：这一端必须安装。
- **可选**：这一端不装也能用；想要它在这一端的功能就装上。
- **不需要**：不要装在这一端；仅客户端模组装在专用服务器上可能导致崩溃。

## 开发

    go test ./...                         # 单元测试，不联网
    MMF_ONLINE=1 go test ./internal/eval  # 额外与真实服务端整合包对比

准确率数据来自 `internal/eval`，它使用 `testdata/corpus/` 和 `testdata/holdout/` 中的 jar。这些是第三方文件，不提交到仓库，可以这样重新获取：

    go run ./tools/fetchcorpus -out testdata/corpus -sets fabric-1.20.1,fabric-1.21.1,forge-1.12.2,forge-1.20.1,neoforge-1.21.1

判断错了？请附上 `mcmodfilter -format json <那个 jar>` 的输出提 issue。

## 计划

- 生成服务端整合包（只复制服务端需要的模组、安装加载器）
- 终端 UI
