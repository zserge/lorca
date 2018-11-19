package main

import (
	"net/url"

	"github.com/zserge/lorca"
)

func main() {
	// Create UI with basic HTML passed via data URI
	ui, _ := lorca.New("data:text/html,"+url.PathEscape(`
	<html>
		<head><title>Hello</title></head>
		<body><h1>Hello, world!</h1></body>
	</html>
	`), "", 480, 320)
	defer ui.Close()
	// Wait until UI window is closed
	<-ui.Done()
}
