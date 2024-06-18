# Mermaid CLI (for SVG), in Go

## Overview

Usage:

```
mermaid-cli [-l] [-w] [-outdir] file.mmd [file2.mmd ...]
```

file.mmd will be rendered to SVG as file.svg, file2.mmd to file2.svg, etc...

## Acknowledgements

I would not have understood the relationship between MermaidJS and the browser without studying <https://github.com/abhinav/goldmark-mermaid/blob/main/mermaidcdp/compiler.go>.

Thank you, Abhinav Gupta!

## Installation

1. Clone this repository, or download the ZIP'ed-up version
2. CD to the project directory
3. Run download.sh to get the latest minified version of the MermaidJS source
4. Run `go install`

## Motivation

I wanted lower latency than the official [mermaid-cli](https://github.com/mermaid-js/mermaid-cli) for a default render of multiple documents to SVG.

Given this minimal MermaidJS document:

```mermaid
flowchart TD
    A[Getting there] -->B{Let me think}
    B -->|One| C[Walk]
    B -->|Two| D[fa:fa-bus fa:fa-train Public transit]
    B -->|Three| E[fa:fa-bicycle Bike]
```

I was seeing ~2s to render with the official mermaid-cli:

```none
/usr/bin/time mmdc -i testdata/flow.mmd -o testdata/flow.svg
Generating single mermaid chart
[@zenuml/core] Store is a function and is not initiated in 1 second.
        1.93 real         1.94 user         0.39 sys
```

With this mermaid-cli it's down to ~500ms:

```none
% /usr/bin/time mermaid-cli testdata/flow.mmd
        0.51 real         0.13 user         0.06 sys
```

The official cli doesn't support (as far as I can see) batching multiple documents, which means that in the background multiple instances of a headless browser have to be spun up, one after the other, for each document.

This mermaid-cli accepts multiple documents (and so can amortize the cost of spinning up the headless browser):

```none
% /usr/bin/time mermaid-cli -l testdata/*.mmd
2024/06/17 12:31:23 starting headless browser
2024/06/17 12:31:24 rendered testdata/flow.svg
2024/06/17 12:31:24 rendered testdata/sequence.svg
2024/06/17 12:31:24 rendered testdata/state.svg
2024/06/17 12:31:24 stopped headless browser
        0.53 real         0.13 user         0.06 sys
```

Log events (with the -l flag) print to standard error.

It also has a simple watch flag that checks the input files every 250ms for new modification times.  If it finds a newly-modified input file it re-renders it:

```none
% mermaid-cli -l -w testdata/*.mmd
2024/06/17 12:31:56 starting headless browser
2024/06/17 12:31:56 rendered testdata/flow.svg
2024/06/17 12:31:56 rendered testdata/sequence.svg
2024/06/17 12:31:56 rendered testdata/state.svg
2024/06/17 12:31:56 watching...
2024/06/17 12:32:04 rendered testdata/flow.svg
2024/06/17 12:32:09 rendered testdata/state.svg
^C
2024/06/17 12:32:12 done watching
2024/06/17 12:32:12 stopped headless browser
```

By default, the cli saves an SVG file in the same directory as its source MermaidJS document:

```none
% mermaid-cli -l a/flow.mmd b/state.mmd
...
2024/06/18 13:18:32 rendered a/flow.svg
2024/06/18 13:18:32 rendered b/state.svg
...
```

The -outdir flag specifies one directory where all SVG files will be saved:

```none
% mermaid-cli -l -outdir=tmp a/flow.mmd b/state.mmd
...
2024/06/18 13:19:03 rendered tmp/flow.svg
2024/06/18 13:19:03 rendered tmp/state.svg
...
```
