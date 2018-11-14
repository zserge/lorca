# Lorca

[![Build Status](https://travis-ci.org/zserge/lorca.svg?branch=master)](https://travis-ci.org/zserge/lorca)
[![GoDoc](https://godoc.org/github.com/zserge/lorca?status.svg)](https://godoc.org/github.com/zserge/lorca)
[![Go Report Card](https://goreportcard.com/badge/github.com/zserge/lorca)](https://goreportcard.com/report/github.com/zserge/lorca)

<div>
<img align="left" src="https://raw.githubusercontent.com/zserge/lorca/master/lorca.png" alt="Lorca" width="128px" height="128px" />
<br/>
<p>
  A very small library to build modern HTML5 desktop apps in Go. It uses Chrome
  as a UI layer. It allows calling Go code from the UI and manipulating UI from
  Go. Internally it implements Chrome Debug Protocol, so theoretically can also
  use other browsers that support it.
</p>
<br/>
</div>


## Example

```go
ui, err := lorca.New("", "", 480, 320)
defer ui.Close()

// Bind Go function to be available in JS. Go function may be long-running and
// blocking - in JS it's represented with a Promise.
ui.Bind("add", func(a, b int) int { return a + b })

// Call JS function from Go. Functions may be asynchronous, i.e. return promises
n := ui.Eval(`Math.random()`).Float()

// Call JS that calls Go and so on and so on...
m := ui.Eval(`add(2, 3)`).Int()

// Wait for the browser window to be closed
<-ui.Done()
```

## So, it's a webview?

I also maintain the [webview](https://github.com/zserge/webview) project.
Although Lorca solves a similar problem (modern GUI in Go), it solves the
problem differently - it doesn't rely on native OS web engine, but instead
requires a modern browser to be installed. For some use cases it's the
preferred option.

Also, I expect webview to remain small and simple, and what is more important,
written in C. Lorca on the other hand should be implemented in pure Go and be
as convenient as possible.

## Good practices

Embed your assets into a single Go binary. At the moment it's beyond the scope
of this library, but might be simplified in future.

I would recommend injecting HTML/JS/CSS using `Eval()` and data URIs. Although,
a local web server would also solve the problem.

I would recommend using an architecture similar to Redux, where Go code would
handle actions from the UI, update internal state and call render function in
JavaScript to apply display the new state in the UI.

Pass "--headless" CLI option to the browser to run automated UI tests.

## What's in a name?

> There is kind of a legend, that before his execution Garcia Lorca have seen a
> sunrise over the heads of the soldiers and he said "And yet, the run rises...".
> Probably it was the beginning of a poem. (J. Brodsky)

Lorca is an anagram of [Carlo](https://github.com/GoogleChromeLabs/carlo/), a
project with a similar goal for Node.js.

## License

Code is distributed under MIT license, feel free to use it in your proprietary
projects as well.

