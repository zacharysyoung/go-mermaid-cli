/*
Mermaid-CLI takes MermaidJS document with an MMD extension,
renders it and saves it to a file with the same name but with
an SVG extension.

usage: mermaid-cli [-l] [-w] file.mmd [file2.mmd ...]

The following was largely cribbed from:
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mermaid-cli [-l] [-w] file.mmd [file2.mmd ...]")
	flag.PrintDefaults()
	os.Exit(2)
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

	for _, inputName := range flag.Args() {
		if !strings.HasSuffix(inputName, mmd) {
			fatalf("got input MermaidJS document %s; expected it to end with %s", inputName, mmd)
		}
	}

	renderer = NewRenderer()
	switch {
	case *watchFlag:
		watchAndRender(flag.Args())
	default:
		for _, inputName := range flag.Args() {
			renderMMD(inputName)
		}
	}
	renderer.Stop()
}

const (
	mmd = ".mmd"
	svg = ".svg"
)

func watchAndRender(inputNames []string) {
	modTime := func(name string) time.Time {
		info, err := os.Stat(name)
		if err != nil {
			fatalf("couldn't get info: %v", err)
		}
		return info.ModTime()
	}

	modTimes := make(map[string]time.Time)
	for _, inputName := range inputNames {
		renderMMD(inputName)
		modTimes[inputName] = modTime(inputName)
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
			for _, inputName := range inputNames {
				t := modTime(inputName)
				if t.After(modTimes[inputName]) {
					renderMMD(inputName)
					modTimes[inputName] = t
				}
			}
		}
	}

	log.Println("done")
	return
}

func renderMMD(inputName string) {
	b, err := os.ReadFile(inputName)
	if err != nil {
		fatalf("couldn't read MMD: %v", err)
	}

	svgResult := renderer.Render(string(b))

	outName := outputName(inputName)
	if err := os.WriteFile(outName, []byte(svgResult), 0644); err != nil {
		fatalf("couldn't write SVG: %v", err)
	}
	log.Println("rendered", outName)
}
func outputName(inputName string) string { return strings.TrimSuffix(inputName, mmd) + svg }

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
// registered with the headles Chrome browser.  mermaid-cli
// uses the minified version (see download.sh) for a smaller
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

// fatalf prints an error to Stderr and exits with return code 2.
// It also adds some extra formatting so the caller doesn't have
// to.
func fatalf(format string, args ...any) {
	enableLogging()
	if !strings.HasPrefix(format, "error: ") {
		format = "error: " + format
	}
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	log.Fatalf(format, args...)
}
