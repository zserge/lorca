package main

import (
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/zserge/lorca"
)

func main() {
	ui, err := lorca.New("data:text/html,<html>Hello</html>", "/tmp/chrome-tmp-datadir", 480, 320)
	if err != nil {
		log.Fatal(err)
	}
	defer ui.Close()

	ui.Bind("rpcCall", func(args []interface{}) (interface{}, error) {
		log.Println("rpc call", args)
		time.Sleep(time.Second)
		return len(args[0].(string)), nil
	})

	ui.Load("https://zserge.com")
	log.Println(ui.Eval("x = {a:3, b:'foo'}"))
	log.Println(ui.Eval("(() => Promise.resolve(42))()"))
	go func() {
		log.Println(ui.Eval("window.rpcCall('hi, world')"))
	}()
	log.Println(ui.Eval("window.rpcCall('hello')"))

	sigc := make(chan os.Signal)
	signal.Notify(sigc, os.Interrupt)
	select {
	case <-sigc:
	case <-ui.Done():
	}
}
