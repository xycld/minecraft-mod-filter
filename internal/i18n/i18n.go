// Package i18n holds the user-facing messages in English and Simplified Chinese.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Lang string

const (
	En   Lang = "en"
	ZhCN Lang = "zh-CN"
)

// Detect picks a language from an explicit choice or the environment.
func Detect(explicit string) Lang {
	if explicit != "" {
		if strings.HasPrefix(strings.ToLower(explicit), "zh") {
			return ZhCN
		}
		return En
	}
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := os.Getenv(v); val != "" {
			if strings.HasPrefix(strings.ToLower(val), "zh") {
				return ZhCN
			}
			return En
		}
	}
	return En
}

// T formats the message for key. Unknown keys fall back to English, then to the key.
func (l Lang) T(key string, args ...any) string {
	msg, ok := catalog[l][key]
	if !ok {
		if msg, ok = catalog[En][key]; !ok {
			msg = key
		}
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

var catalog = map[Lang]map[string]string{
	En: {
		// Evidence rules.
		"meta.environment.client":       `%s declares environment "client"`,
		"meta.environment.server":       `%s declares environment "server"`,
		"meta.entrypoints.client":       "%s has only client entrypoints",
		"meta.entrypoints.server":       "%s has only server entrypoints",
		"meta.clientSideOnly":           "%s sets clientSideOnly=true",
		"meta.displayTest.ignoreServer": "%[2]s: displayTest=IGNORE_SERVER_VERSION (clients without it can join)",
		"meta.displayTest.ignoreAll":    "%[2]s: displayTest=IGNORE_ALL_VERSION (works when the other side lacks it)",
		"meta.depSide.client":           "%s: depends on the game on the client side only",
		"meta.depSide.server":           "%s: depends on the game on the server side only",
		"class.mod.client":              "@Mod class %s loads only on the client",
		"class.mod.server":              "@Mod class %s loads only on the dedicated server",
		"class.mod.anyRemote":           "@Mod accepts any version on the other side",
		"mixin.clientOnly":              "all %s mixins target the client",
		"mixin.serverOnly":              "all %s mixins are server-only",
		"mixin.common":                  "%s mixins change shared game code",
		"res.dataOnly":                  "data pack only (%s data files, no code)",
		"res.assetsOnly":                "resource pack only (no code)",
		"res.both":                      "resources and data only (no code)",
		"res.content":                   "adds blocks/items (%s blockstates, %s item models)",
		"res.worldgen":                  "%s world generation files",
		"class.registersContent":        "registers game content",
		"class.noClient":                "none of %s game classes use client code",
		"class.allClient":               "all %s game classes are client code",
		"class.mostlyClient":            "%s of %s game classes are client code",
		"class.hasClient":               "%s classes contain client code",
		"class.commonLogic":             "%s classes contain shared game logic",
		"class.networking.optional":     "has a network protocol a server can use",
		"class.networking":              "registers network packets",
		"nested.combined":               "combined verdict of %s bundled jars",
		"fallback.both":                 "no signals found; assuming both sides",
		"modrinth.environment":          "Modrinth: %s",
		"modrinth.project":              "Modrinth project: client %s, server %s",
		"modrinth.slug":                 "Modrinth project %s (matched by mod id): %s",
		"curseforge.tags":               "CurseForge environment tags",
		"pack.declared":                 "declared by the modpack",
		"deps.requiredBy.server":        "required on the server by %s",
		"deps.requiredBy.client":        "required on the client by %s",
		"deps.notNeededOnServer":        "library used only by mods the server does not need: %s",

		// Categories and needs.
		"cat.client-only":              "client-only",
		"cat.server-only":              "server-only",
		"cat.both":                     "both",
		"cat.either":                   "either side",
		"cat.client (server optional)": "client (server optional)",
		"cat.server (client optional)": "server (client optional)",
		"cat.unknown":                  "unknown",
		"cat.unsupported":              "unsupported",
		"need.required":                "required",
		"need.optional":                "optional",
		"need.unsupported":             "unsupported",
		"need.unknown":                 "unknown",
		"conf.high":                    "high",
		"conf.medium":                  "medium",
		"conf.low":                     "low",

		// Report.
		"col.mod":                "Mod",
		"col.file":               "File",
		"col.client":             "Client",
		"col.server":             "Server",
		"col.confidence":         "Confidence",
		"col.reason":             "Main reason",
		"summary.title":          "Summary",
		"summary.total":          "%d mods scanned (%s)",
		"summary.online":         "online",
		"summary.offline":        "offline",
		"summary.removeServer":   "Remove from a dedicated server (%d):",
		"summary.optionalServer": "Optional on a server, nothing on it depends on them (%d):",
		"summary.removeClient":   "Remove from clients (%d):",
		"summary.lowConfidence":  "Low confidence, please check (%d):",
		"summary.missing":        "Missing dependencies: %s",
		"summary.warnings":       "Warnings:",
		"summary.problems":       "Problems:",
		"evidence":               "Evidence",
		"requiredBy":             "required by",
	},
	ZhCN: {
		"meta.environment.client":       `%s 声明 environment 为 "client"`,
		"meta.environment.server":       `%s 声明 environment 为 "server"`,
		"meta.entrypoints.client":       "%s 只有客户端入口点",
		"meta.entrypoints.server":       "%s 只有服务端入口点",
		"meta.clientSideOnly":           "%s 设置了 clientSideOnly=true",
		"meta.displayTest.ignoreServer": "%[2]s: displayTest=IGNORE_SERVER_VERSION（没装的客户端也能进服）",
		"meta.displayTest.ignoreAll":    "%[2]s: displayTest=IGNORE_ALL_VERSION（另一端可以不装）",
		"meta.depSide.client":           "%s: 只在客户端依赖游戏本体",
		"meta.depSide.server":           "%s: 只在服务端依赖游戏本体",
		"class.mod.client":              "@Mod 类 %s 只在客户端加载",
		"class.mod.server":              "@Mod 类 %s 只在专用服务器加载",
		"class.mod.anyRemote":           "@Mod 接受另一端任意版本",
		"mixin.clientOnly":              "全部 %s 个 mixin 都作用于客户端",
		"mixin.serverOnly":              "全部 %s 个 mixin 都只用于服务端",
		"mixin.common":                  "%s 个 mixin 修改双端共用的游戏代码",
		"res.dataOnly":                  "纯数据包（%s 个数据文件，无代码）",
		"res.assetsOnly":                "纯资源包（无代码）",
		"res.both":                      "只有资源和数据（无代码）",
		"res.content":                   "添加了方块/物品（%s 个方块状态，%s 个物品模型）",
		"res.worldgen":                  "%s 个世界生成文件",
		"class.registersContent":        "注册了游戏内容",
		"class.noClient":                "%s 个游戏类都不使用客户端代码",
		"class.allClient":               "%s 个游戏类全部是客户端代码",
		"class.mostlyClient":            "%s/%s 个游戏类是客户端代码",
		"class.hasClient":               "%s 个类包含客户端代码",
		"class.commonLogic":             "%s 个类包含双端游戏逻辑",
		"class.networking.optional":     "带有网络协议，服务端可以配合使用",
		"class.networking":              "注册了网络数据包",
		"nested.combined":               "综合 %s 个内嵌 jar 的结果",
		"fallback.both":                 "没有找到任何线索，默认双端",
		"modrinth.environment":          "Modrinth: %s",
		"modrinth.project":              "Modrinth 项目: 客户端 %s，服务端 %s",
		"modrinth.slug":                 "Modrinth 项目 %s（按 mod id 匹配）: %s",
		"curseforge.tags":               "CurseForge 环境标签",
		"pack.declared":                 "整合包中声明",
		"deps.requiredBy.server":        "服务端上的 %s 依赖它",
		"deps.requiredBy.client":        "客户端上的 %s 依赖它",
		"deps.notNeededOnServer":        "前置库，只被服务端不需要的模组使用: %s",

		"cat.client-only":              "仅客户端",
		"cat.server-only":              "仅服务端",
		"cat.both":                     "双端",
		"cat.either":                   "任意一端",
		"cat.client (server optional)": "客户端（服务端可选）",
		"cat.server (client optional)": "服务端（客户端可选）",
		"cat.unknown":                  "未知",
		"cat.unsupported":              "不支持",
		"need.required":                "必需",
		"need.optional":                "可选",
		"need.unsupported":             "不需要",
		"need.unknown":                 "未知",
		"conf.high":                    "高",
		"conf.medium":                  "中",
		"conf.low":                     "低",

		"col.mod":                "模组",
		"col.file":               "文件",
		"col.client":             "客户端",
		"col.server":             "服务端",
		"col.confidence":         "可信度",
		"col.reason":             "主要依据",
		"summary.title":          "汇总",
		"summary.total":          "共扫描 %d 个模组（%s）",
		"summary.online":         "在线",
		"summary.offline":        "离线",
		"summary.removeServer":   "应从专用服务器移除（%d）:",
		"summary.optionalServer": "服务端可选，且没有其它模组依赖（%d）:",
		"summary.removeClient":   "应从客户端移除（%d）:",
		"summary.lowConfidence":  "可信度低，请手动确认（%d）:",
		"summary.missing":        "缺少前置: %s",
		"summary.warnings":       "警告:",
		"summary.problems":       "问题:",
		"evidence":               "依据",
		"requiredBy":             "被依赖于",
	},
}
