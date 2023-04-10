package main

import (
	"log"
	"os"

	"github.com/zserge/lorca"
)

func main() {
	var pwd, e = os.Getwd()
	if e != nil {
		panic(e)
	}

	lorca.Preferences = nil

	// Create UI with basic HTML passed via data URI
	ui, err := lorca.New("https://github.com", pwd+"/lorca_passwd_temp", 480, 320, "--remote-allow-origins=*")
	if err != nil {
		log.Fatal(err)
	}
	defer ui.Close()
	// Wait until UI window is closed
	<-ui.Done()
}
