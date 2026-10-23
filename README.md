<p align="center">
    <img src="./icon.png">
</p>

<h1 align="center">Minecraft mod filter</h1>
<p align="center">English | <a href="README_zh-hans.md">简体中文</a></p>

Point it at a mods folder or a modpack and it tells you, for every mod, whether the **client** and the **server** need it — so you can build a server pack without guesswork, or strip server-only mods from a client.

```
$ mcmodfilter ~/.minecraft/mods
Mod                      Client    Server       Confidence  Main reason
Sodium                   required  unsupported  high        @Mod class …/SodiumForgeMod loads only on the client
EMI                      required  optional     medium      Modrinth: client_only_server_optional
Lithium                  optional  optional     high        Modrinth: client_or_server_prefers_both
Create                   required  required     high        adds blocks/items (644 blockstates, 785 item models)
Architectury             required  optional     medium      library used only by mods the server does not need: OctoLib
YUNG's Better Strongholds optional required     high        Modrinth: server_only
...
Remove from a dedicated server (21):
  sodium-neoforge-0.6.13+mc1.21.1.jar
  ...
```

## Features

- **Every loader**: Forge (1.12 `mcmod.info` through modern `mods.toml`), NeoForge, Fabric and Quilt, including multi-loader jars and mods bundled inside other jars.
- **Reads the jar, not just the name**: loader metadata, `@Mod(dist = …)` / `clientSideOnly` annotations, mixin configs and their targets, which classes touch client-only code, and whether the mod ships blocks, items or world generation.
- **Exact platform lookups**: files are matched on Modrinth by hash (no fuzzy name guessing) and, with an API key, on CurseForge by fingerprint.
- **Understands dependencies**: a library needed by a server mod stays on the server; a library only client mods use is marked optional there.
- **Explains itself**: every verdict comes with a confidence level and the evidence behind it (`-explain`).
- **Works offline** (`-offline`) with nothing but the jars.
- Reads mods folders, single jars, Modrinth `.mrpack` and CurseForge modpack `.zip` files.
- Table, JSON and Markdown output; English and 简体中文.
- One static binary for Windows, macOS and Linux. It never runs code from the jars it reads.

## Accuracy

Measured on 707 real mod jars with author-declared side labels, counting only mistakes that would matter: removing a mod a side needs, or keeping one that cannot run there.

| Corpus | Jars | Correct, offline |
|---|---|---|
| Fabric 1.20.1 / 1.21.1, Forge 1.12.2 / 1.20.1, NeoForge 1.21.1, a real NeoForge pack | 407 | 97.8% |
| Held-out versions never used for tuning: Fabric 1.19.2, Quilt 1.20.1, Forge 1.16.5 / 1.18.2, NeoForge 1.20.4 | 300 | 97.7% |

Online lookups add the mod authors' own declarations on top. On the *Smallish Bits* NeoForge pack, the mods it would keep on a server match the pack author's official server pack for 90.7% of mods; the differences are optional mods and three client-only mods the official pack ships anyway.

## Install

Download a binary from [Releases](https://github.com/xycld/minecraft-mod-filter/releases), or build it with Go 1.26+:

    go install github.com/xycld/minecraft-mod-filter/cmd/mcmodfilter@latest

## Usage

    mcmodfilter [flags] <path>

`<path>` is a mods folder (or an instance folder containing `mods/`), a single `.jar`, a `.mrpack` or a CurseForge `.zip`. Without a path it scans `./mods`.

| Flag | |
|---|---|
| `-format table\|json\|markdown` | output format (default `table`) |
| `-explain` | list every piece of evidence behind each verdict |
| `-offline` | analyse the jars only, no network |
| `-loader fabric\|quilt\|forge\|neoforge` | which loader's metadata to trust in multi-loader jars (default: detected) |
| `-curseforge-key KEY` | enable CurseForge lookups (or set `CURSEFORGE_API_KEY`) |
| `-download` | download the files a `.mrpack`/CurseForge pack references and analyse them too |
| `-lang en\|zh-CN` | output language (default: from `LANG`) |
| `-no-cache` | skip the API response cache (kept for 7 days in your user cache directory) |

How to read the result:

- **required** — the side must have the mod.
- **optional** — the mod works without it on that side; keep it if you want its features there.
- **unsupported** — leave it off that side; a client-only mod on a dedicated server can crash it.

## Development

    go test ./...                      # unit tests, no network
    MMF_ONLINE=1 go test ./internal/eval  # also compares against a real server pack

The accuracy figures come from `internal/eval`, which runs against jars in `testdata/corpus/` and `testdata/holdout/`. They are third-party files and not committed; recreate them with:

    go run ./tools/fetchcorpus -out testdata/corpus -sets fabric-1.20.1,fabric-1.21.1,forge-1.12.2,forge-1.20.1,neoforge-1.21.1

Wrong verdict? Please open an issue with the output of `mcmodfilter -format json <the jar>`.

## Roadmap

- Build a server pack (copy only what the server needs, install the loader)
- A terminal UI
