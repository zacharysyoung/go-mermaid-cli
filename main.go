/*
Mermaid-CLI takes MermaidJS documents with a .mmd extension and
renders them to SVG files with the same name but with a .svg
extension.

usage: mermaid-cli [-l] [-w] file.mmd [file2.mmd ...]

The following was inspired by:
https://github.com/abhinav/goldmark-mermaid/blob/main/mermaidcdp/compiler.go
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "embed"

	cdruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

var (
	watchFlag = flag.Bool("w", false, "watch files and render")
	logFlag   = flag.Bool("l", false, "turn on logging")

	renderer svgRenderer
)

const (
	mmd = ".mmd"
	svg = ".svg"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mermaid-cli [-l] [-w] file.mmd [file2.mmd ...]")
	flag.PrintDefaults()
	os.Exit(2)
}

// renderPair holds the names of the input MermaidJS document
// and output SVG file.
type renderPair struct {
	mmdName, svgName string
}

func main() {
	flag.Usage = usage
	flag.Parse()
	if len(flag.Args()) < 1 {
		usage()
	}

	switch {
	default:
		log.SetFlags(0)
		log.SetOutput(io.Discard)
	case *logFlag:
		enableLogging()
	}

	pairs := make([]renderPair, 0)
	for _, inputName := range flag.Args() {
		if !strings.HasSuffix(inputName, mmd) {
			fatalf("got input MermaidJS document %s; expected it to end with %s", inputName, mmd)
		}
		pairs = append(pairs, renderPair{
			mmdName: inputName,
			svgName: strings.TrimSuffix(inputName, mmd) + svg,
		})
	}

	renderer = NewRenderer()
	switch {
	case *watchFlag:
		watchAndRender(pairs)
	default:
		for _, pair := range pairs {
			render(pair)
		}
	}
	renderer.Stop()
}

// watchAndRender immediately renders the MermaidJS documents in
// inputNames and sets up a watcher to rerender the documents if
// they change.
//
// The watcher polls all files every 250ms.  It prints and exits
// for any error.
func watchAndRender(pairs []renderPair) {
	modTime := func(name string) time.Time {
		info, err := os.Stat(name)
		if err != nil {
			fatalf("couldn't get info: %v", err)
		}
		return info.ModTime()
	}

	modTimes := make(map[string]time.Time)
	for _, pair := range pairs {
		render(pair)
		modTimes[pair.mmdName] = modTime(pair.mmdName)
	}

	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(250 * time.Millisecond)

	log.Println("watching...")

Loop:
	for {
		select {
		case <-stop:
			fmt.Fprintln(os.Stdout)
			break Loop
		case <-ticker.C:
			for _, pair := range pairs {
				t := modTime(pair.mmdName)
				if t.After(modTimes[pair.mmdName]) {
					modTimes[pair.mmdName] = t
					render(pair)
				}
			}
		}
	}

	log.Println("done")
	return
}

// render renders the MermaidJS document at pair.mmdName to
// SVG at pair.svgName.
//
// It prints and exits for any error.
func render(pair renderPair) {
	b, err := os.ReadFile(pair.mmdName)
	if err != nil {
		fatalf("couldn't read MMD: %v", err)
	}

	svgResult := renderer.Render(string(b))

	if err := os.WriteFile(pair.svgName, []byte(svgResult), 0644); err != nil {
		fatalf("couldn't write SVG: %v", err)
	}
	log.Println("rendered", pair.svgName)
}

// svgRenderer manages the setup and teardown of the headeless
// Chrome browser, and the rendering of a MermaidJS document.
type svgRenderer struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// mermaidInitializeConfig fulfills some basic requirements for
// using MermaidJS.
type mermaidInitializeConfig struct {
	Theme       string `json:"theme,omitempty"`
	StartOnLoad bool   `json:"startOnLoad"`
}

// mermaidJSSource is the source for MermaidJS that will be
// registered with the headles Chrome browser.
//
// Use the minified version (see download.sh) for a smaller
// binary.
//
//go:embed mermaid.min.js
var mermaidJSSource string

// extrasJSSource has helper code that will be registered with
// the browser, in addition to mermaidJSSource.
//
//   - renderSVG calls MermaidJS's render func, and will be called
//     by the Render method.
const extrasJSSource = `
async function renderSVG(src) {
		const { svg } = await mermaid.render('mermaid', src);
		return svg;
}
`

// NewRenderer starts a headless Chrome browser and sets up
// MermaidJS with that browser.
//
// Prints and exits for any error.
func NewRenderer() svgRenderer {
	log.Println("starting headless browser")

	// Start Chrome
	ctx, cancel := chromedp.NewContext(context.Background())

	var ready *cdruntime.RemoteObject

	// Load MermaidJS in browser
	if err := chromedp.Run(ctx, chromedp.Evaluate(mermaidJSSource, &ready)); err != nil {
		fatalf("set up headless browser: %w", err)
	}

	// Initialize MermaidJS
	initConfig := mermaidInitializeConfig{
		Theme:       "default",
		StartOnLoad: false,
	}

	jsSource := jsonEncodeJS("mermaid.initialize(", initConfig, ")")
	ready = nil
	if err := chromedp.Run(ctx, chromedp.Evaluate(jsSource, &ready)); err != nil {
		fatalf("initialize mermaid: %w", err)
	}

	// Load helpers in browser
	ready = nil
	if err := chromedp.Run(ctx, chromedp.Evaluate(extrasJSSource, &ready)); err != nil {
		fatalf("inject additional JavaScript: %w", err)
	}

	return svgRenderer{ctx, cancel}
}

// Render calls the extras renderSVG func to render mmdSource to
// SVG.
//
// Prints and exits for any error.
func (r svgRenderer) Render(mmdSource string) (svgResult string) {
	jsSource := jsonEncodeJS("renderSVG(", mmdSource, ")")

	render := chromedp.Evaluate(
		jsSource,
		&svgResult,
		func(p *cdruntime.EvaluateParams) *cdruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		},
	)

	if err := chromedp.Run(r.ctx, render); err != nil {
		fatalf("couldn't render: %v", err)
	}

	return svgResult
}

// Stop stops the headless Chrome browser.
func (r svgRenderer) Stop() { r.cancel() }

// jsonEncodeJS JSON-encodes encodable, and wraps it in pre and
// post... presumably to make it ready for from chromedp to send
// in a JSON body... maybe jsonEscapeJS would be more apt.
func jsonEncodeJS(pre string, encodable any, post string) string {
	var jsSource strings.Builder

	jsSource.WriteString(pre)
	if err := json.NewEncoder(&jsSource).Encode(encodable); err != nil {
		fatalf("encode source: %w", err)
	}
	jsSource.WriteString(post)

	return jsSource.String()
}

func enableLogging() {
	log.SetFlags(3)
	log.SetOutput(os.Stderr)
}

// fatalf logs the format string and its arguments to Stderr and
// exits with return code 1.
//
// It also adds some extra formatting so the caller doesn't have
// to.
func fatalf(format string, args ...any) {
	if !strings.HasPrefix(format, "error: ") {
		format = "error: " + format
	}
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	enableLogging()
	log.Fatalf(format, args...)
}
