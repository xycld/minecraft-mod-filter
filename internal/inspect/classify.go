package inspect

import (
	"strings"

	"github.com/xycld/minecraft-mod-filter/internal/classfile"
)

// Packages that only exist on the physical client.
var clientPrefixes = []string{
	"net/minecraft/client/",
	"com/mojang/blaze3d/",
	"com/mojang/realmsclient/",
	"net/minecraft/realms/",
	"net/minecraftforge/client/",
	"net/minecraftforge/fml/client/",
	"net/neoforged/neoforge/client/",
	"net/fabricmc/fabric/api/client/",
	"net/fabricmc/fabric/impl/client/",
	"org/quiltmc/qsl/base/api/entrypoint/client/",
	"org/lwjgl/opengl/",
	"org/lwjgl/glfw/",
	// APIs of mods that only run on the client: recipe viewers, mod menus, config screens.
	"mezz/jei/",
	"dev/emi/emi/",
	"me/shedaniel/rei/",
	"com/terraformersmc/modmenu/",
	"io/github/prospector/modmenu/",
	"me/shedaniel/clothconfig2/",
	"dev/isxander/yacl3/",
}

// Packages and classes whose use means the mod has game logic outside the client.
var gamePrefixes = []string{
	"net/minecraft/",
	"com/mojang/blaze3d/",
	"net/minecraftforge/",
	"net/neoforged/",
	"net/fabricmc/",
	"org/quiltmc/",
	"cpw/mods/fml/",
}

var serverPrefixes = []string{
	"net/minecraft/server/",
	"net/minecraft/class_3218", // ServerWorld (intermediary)
	"net/minecraft/class_3222", // ServerPlayerEntity (intermediary)
}

var registryClasses = []string{
	"net/neoforged/neoforge/registries/DeferredRegister",
	"net/neoforged/neoforge/registries/RegisterEvent",
	"net/minecraftforge/registries/DeferredRegister",
	"net/minecraftforge/registries/RegisterEvent",
	"net/minecraftforge/event/RegistryEvent$Register",
	"net/minecraftforge/fml/common/registry/GameRegistry",
	"net/minecraftforge/fml/common/registry/EntityRegistry",
	"cpw/mods/fml/common/registry/GameRegistry",
	"net/minecraft/class_2378", // Registry (intermediary)
	"net/fabricmc/fabric/api/object/builder/",
	"dev/architectury/registry/registries/DeferredRegister",
}

var networkClasses = []string{
	"net/neoforged/neoforge/network/registration/PayloadRegistrar",
	"net/neoforged/neoforge/network/event/RegisterPayloadHandlersEvent",
	"net/minecraftforge/network/simple/SimpleChannel",
	"net/minecraftforge/network/NetworkRegistry",
	"net/minecraftforge/network/ChannelBuilder",
	"net/minecraftforge/fml/common/network/simpleimpl/SimpleNetworkWrapper",
	"net/minecraftforge/fml/common/network/NetworkRegistry",
	"net/fabricmc/fabric/api/networking/v1/ServerPlayNetworking",
	"net/fabricmc/fabric/api/networking/v1/PayloadTypeRegistry",
	"dev/architectury/networking/NetworkManager",
}

var commandClasses = []string{
	"net/neoforged/neoforge/event/RegisterCommandsEvent",
	"net/minecraftforge/event/RegisterCommandsEvent",
	"net/minecraftforge/fml/common/event/FMLServerStartingEvent",
	"net/fabricmc/fabric/api/command/v2/CommandRegistrationCallback",
	"net/fabricmc/fabric/api/command/v1/CommandRegistrationCallback",
}

// IsClientClass reports whether a referenced class exists only on the client.
func IsClientClass(name string) bool {
	return hasAnyPrefix(name, clientPrefixes)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func allClient(names []string) bool {
	for _, n := range names {
		if !IsClientClass(n) {
			return false
		}
	}
	return true
}

func (j *Jar) classify(c *classfile.Class) *classInfo {
	ci := &classInfo{name: c.Name}
	clientRef, gameRef, serverRef := false, false, false
	for _, r := range c.Refs {
		if IsClientClass(r) {
			clientRef = true
		}
		if hasAnyPrefix(r, gamePrefixes) {
			gameRef = true
		}
		if hasAnyPrefix(r, serverPrefixes) {
			serverRef = true
		}
		if hasAnyPrefix(r, registryClasses) {
			j.Classes.Registers = true
		}
		if hasAnyPrefix(r, networkClasses) {
			j.Classes.Networking = true
		}
		if hasAnyPrefix(r, commandClasses) {
			j.Classes.Commands = true
		}
	}
	ci.game = gameRef
	if serverRef {
		j.Classes.Server++
	}
	ci.client = clientRef || clientPackage(c.Name)

	for _, a := range c.Annotations {
		switch a.Type {
		case "org/spongepowered/asm/mixin/Mixin":
			ci.isMixin = true
			for _, v := range list(a.Values["value"]) {
				if cv, ok := v.(classfile.ClassValue); ok {
					ci.mixinTargets = append(ci.mixinTargets, string(cv))
				}
			}
			for _, v := range list(a.Values["targets"]) {
				if s, ok := v.(string); ok {
					ci.mixinTargets = append(ci.mixinTargets, strings.ReplaceAll(s, ".", "/"))
				}
			}
		case "net/neoforged/api/distmarker/OnlyIn", "net/minecraftforge/api/distmarker/OnlyIn",
			"net/minecraftforge/fml/relauncher/SideOnly", "cpw/mods/fml/relauncher/SideOnly",
			"net/fabricmc/api/Environment":
			if enumIs(a.Values["value"], "CLIENT") {
				ci.client = true
			}
		case "net/neoforged/fml/common/EventBusSubscriber", "net/neoforged/fml/common/Mod$EventBusSubscriber",
			"net/minecraftforge/fml/common/Mod$EventBusSubscriber":
			if dists := enumNames(a.Values["value"]); len(dists) == 1 && dists[0] == "CLIENT" {
				ci.client = true
			}
		case "net/neoforged/fml/common/Mod", "net/minecraftforge/fml/common/Mod", "cpw/mods/fml/common/Mod":
			m := ModAnnotation{Class: c.Name, Dist: enumNames(a.Values["dist"])}
			m.ModID, _ = a.Values["value"].(string)
			if m.ModID == "" {
				m.ModID, _ = a.Values["modid"].(string)
			}
			m.ClientSideOnly, _ = a.Values["clientSideOnly"].(bool)
			m.ServerSideOnly, _ = a.Values["serverSideOnly"].(bool)
			m.AcceptableRemoteVersions, _ = a.Values["acceptableRemoteVersions"].(string)
			j.Classes.ModAnnotations = append(j.Classes.ModAnnotations, m)
		}
	}
	return ci
}

// clientPackage reports whether a mod's own class lives in a client package,
// e.g. "com/example/client/Renderer".
func clientPackage(name string) bool {
	return strings.Contains(name, "/client/") || strings.HasSuffix(strings.ToLower(name), "client")
}

func list(v any) []any {
	switch v := v.(type) {
	case []any:
		return v
	case nil:
		return nil
	default:
		return []any{v}
	}
}

func enumNames(v any) []string {
	var out []string
	for _, e := range list(v) {
		if ev, ok := e.(classfile.Enum); ok {
			out = append(out, ev.Name)
		}
	}
	return out
}

func enumIs(v any, name string) bool {
	names := enumNames(v)
	return len(names) == 1 && names[0] == name
}
