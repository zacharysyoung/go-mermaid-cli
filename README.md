# Mermaid CLI (for SVG), in Go

## Overview

Usage:

```
mermaid-cli [-l] [-w] file.mmd [file2.mmd ...]
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

Given this script to render the flow.mmd, sequence.mmd, and state.mmd files in [testdata](./testdata):

```sh
#!/bin/sh

ls testdata/*.mmd | while read MMD; do
	svgName=$(echo $MMD | sed 's/\.mmd/\.svg/')
	mmdc -q -i $MMD -o $svgName
done
```

I was seeing latency of ~2s for each document:

```none
% /usr/bin/time sh render.sh
[@zenuml/core] Store is a function and is not initiated in 1 second.
[@zenuml/core] Store is a function and is not initiated in 1 second.
[@zenuml/core] Store is a function and is not initiated in 1 second.
        5.67 real         5.71 user         1.20 sys
```

The puppeteer JS code and built-in Chromium browser have to be started for each input file. Also, there's presently some error in MermaidJS itself and the error message leaks out and cannot be suppressed (without `... > /dev/null`).

This project uses a user-installed installed Chrome and communicates directly with it through the Chrome Devtools Protocol. It also allows passing multiple input files, to reuse a spun-up headless browswer:

```none
% usr/bin/time mermaid-cli -l testdata/*.mmd
2024/06/17 11:16:29 starting headless browser
2024/06/17 11:16:30 rendered testdata/flow.svg
2024/06/17 11:16:30 rendered testdata/sequence.svg
2024/06/17 11:16:30 rendered testdata/state.svg
        0.53 real         0.13 user         0.07 sys
```

Log events (with the -l flag) print to standard error.

It also has a simple watch flag that monitors (every 250ms) the input files for a new modification times and automatically re-renders:

```none
% mermaid-cli -l -w testdata/*.mmd
2024/06/17 10:40:31 starting headless browser
2024/06/17 10:40:32 rendered testdata/flow.svg
2024/06/17 10:40:32 rendered testdata/sequence.svg
2024/06/17 10:40:32 rendered testdata/state.svg
2024/06/17 10:40:32 watching...
2024/06/17 10:40:39 rendered testdata/state.svg
2024/06/17 10:40:43 rendered testdata/state.svg
^C
2024/06/17 10:40:47 done
```
