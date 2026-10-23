// Command mcmodfilter tells which Minecraft mods belong on the client, the
// server or both.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/xycld/minecraft-mod-filter/internal/cache"
	"github.com/xycld/minecraft-mod-filter/internal/detect"
	"github.com/xycld/minecraft-mod-filter/internal/i18n"
	"github.com/xycld/minecraft-mod-filter/internal/pack"
	"github.com/xycld/minecraft-mod-filter/internal/provider/curseforge"
	"github.com/xycld/minecraft-mod-filter/internal/provider/modrinth"
	"github.com/xycld/minecraft-mod-filter/internal/report"
	"github.com/xycld/minecraft-mod-filter/internal/scan"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `mcmodfilter - find out which Minecraft mods are client-only, server-only or needed on both sides

Usage:
  mcmodfilter [flags] <path>

<path> is a mods folder (or an instance folder containing mods/), a single
.jar, a Modrinth .mrpack or a CurseForge modpack .zip. Defaults to "mods" in
the current directory.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcmodfilter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		format  = fs.String("format", "table", "output format: table, json or markdown")
		offline = fs.Bool("offline", false, "do not query Modrinth/CurseForge; analyse jars only")
		loader  = fs.String("loader", "", "target loader for multi-loader jars: fabric, quilt, forge or neoforge (default: detect)")
		cfKey   = fs.String("curseforge-key", os.Getenv("CURSEFORGE_API_KEY"), "CurseForge API key (or set CURSEFORGE_API_KEY)")
		dl      = fs.Bool("download", false, "download files referenced by .mrpack/CurseForge packs for deeper local analysis")
		explain = fs.Bool("explain", false, "show every piece of evidence behind each verdict")
		lang    = fs.String("lang", "", "language: en or zh-CN (default: from LANG)")
		noCache = fs.Bool("no-cache", false, "do not read or write the API response cache")
		noColor = fs.Bool("no-color", false, "disable colored output")
		workers = fs.Int("workers", 8, "number of jars analysed in parallel")
		showVer = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVer {
		fmt.Fprintln(stdout, "mcmodfilter", version)
		return 0
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 2
	}
	path := "mods"
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	switch *format {
	case "table", "json", "markdown":
	default:
		fmt.Fprintf(stderr, "unknown format %q\n", *format)
		return 2
	}
	switch detect.Loader(*loader) {
	case "", detect.Fabric, detect.Quilt, detect.Forge, detect.NeoForge:
	default:
		fmt.Fprintf(stderr, "unknown loader %q\n", *loader)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pk, err := pack.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	defer pk.Close()

	ua := "xycld/minecraft-mod-filter/" + version + " (github.com/xycld/minecraft-mod-filter)"
	opts := scan.Options{Loader: detect.Loader(*loader), Download: *dl, Workers: *workers}
	if !*offline {
		opts.Modrinth = modrinth.New(ua)
		if *cfKey != "" {
			opts.CurseForge = curseforge.New(*cfKey, ua)
		}
	}
	if !*noCache && !*offline {
		if p, err := cache.DefaultPath(); err == nil {
			opts.Cache = cache.Open(p, 7*24*time.Hour)
		}
	}
	if isTerminal(stderr) {
		opts.Progress = progressPrinter(stderr)
	}

	res, err := scan.Run(ctx, pk, opts)
	if opts.Progress != nil {
		fmt.Fprint(stderr, "\r\x1b[K")
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := opts.Cache.Save(); err != nil {
		fmt.Fprintln(stderr, "warning: saving cache:", err)
	}

	ro := report.Options{
		Lang:    i18n.Detect(*lang),
		Explain: *explain,
		Color:   !*noColor && os.Getenv("NO_COLOR") == "" && isTerminal(stdout),
	}
	switch *format {
	case "json":
		if err := report.JSON(stdout, res); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	case "markdown":
		report.Markdown(stdout, res, ro)
	default:
		report.Table(stdout, res, ro)
	}
	return 0
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func progressPrinter(w io.Writer) func(stage string, done, total int) {
	return func(stage string, done, total int) {
		bar := ""
		if total > 0 {
			n := 20 * done / total
			bar = "[" + strings.Repeat("#", n) + strings.Repeat(".", 20-n) + "]"
		}
		fmt.Fprintf(w, "\r\x1b[K%-9s %s %d/%d", stage, bar, done, total)
	}
}
