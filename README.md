<p align="center">
    <img src="./icon.png">
</p>

<h1 align="center">Minecraft mod filter</h1>

Quickly filter the minecraft modpack to see if any of its mods are client-side/server-side.
## Features
👻 Quickly scans your mods and tells you their server/client compatibility, with the reasons behind every verdict.  
🎯 About 98% accurate even offline: it reads each jar's metadata, `@Mod` annotations, mixins and bytecode, then checks Modrinth (and CurseForge, with an API key) by file hash.  
🧩 Supports Forge (1.12 and newer), NeoForge, Fabric and Quilt, and understands dependencies between mods.  
📦 Scans mods folders, single jars, Modrinth `.mrpack` and CurseForge modpack `.zip` files.  
💎 One small executable for Windows, macOS and Linux, nothing to install.  
🎃 i18n support (English and 简体中文)
## Todo
✨ ~~Support Java reflection to read @mod annotations~~  
✨ ~~Faster file processing with rust~~ (rewritten in Go)  
✨ ~~Add a CLI for terminal users~~  
✨ Auto-generate server modpack (including auto-installation of modloader cores, etc.)  
✨ Make a server-side launcher  
✨ Make a server-side launcher-cli  
✨ Debug  
✨ ~~i18n support~~  
✨ Make a terminal UI
## Known issues
Some old Forge 1.12 core-mods carry almost no metadata; they are marked "low confidence, please check". If a mod is classified wrong, please open an issue with the output of `mcmodfilter -format json <the jar>`.
## How to use
### For customer
Download the file for your system from https://github.com/xycld/minecraft-mod-filter/releases and run it on your mods folder or modpack:

    mcmodfilter path/to/mods

args: `-h`, `-explain`, `-offline`, `-format table|json|markdown`, `-lang en|zh-CN`, `-loader fabric|quilt|forge|neoforge`, `-curseforge-key KEY`, `-download`  
In the result, **required** means the side needs the mod, **optional** means it works without it, and **unsupported** means leave it off that side (a client-only mod can crash a server).
### For Developer
    go run ./cmd/mcmodfilter path/to/mods
    go test ./...

Requires Go 1.26 or newer. To publish a release, push a tag such as `v2.0.0` on master.
### You are welcome to pull requests to help me finish Todo or fix bugs.
##### If you like this project, please give me a free star⭐!

Powered by Go and ❤️
