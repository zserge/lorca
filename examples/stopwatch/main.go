package main

import (
	"fmt"
	"log"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/zserge/lorca"
)

func main() {
	ui, err := lorca.New("", "", 480, 320)
	if err != nil {
		log.Fatal(err)
	}
	defer ui.Close()

	// Data model: number of ticks
	ticks := uint32(0)
	// Channel to connect UI events with the background ticking goroutine
	togglec := make(chan bool)
	// Bind Go functions to JS
	ui.Bind("toggle", func() { togglec <- true })
	ui.Bind("reset", func() {
		atomic.StoreUint32(&ticks, 0)
		ui.Eval(`document.querySelector('.timer').innerText = '0'`)
	})

	// Load HTML after Go functions are bound to JS
	ui.Load("data:text/html," + url.PathEscape(`
	<html>
		<body>
			<!-- toggle() and reset() are Go functions wrapped into JS -->
			<div class="timer" onclick="toggle()"></div>
			<button onclick="reset()">Reset</button>
		</body>
	</html>
	`))

	// Start ticker goroutine
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		for {
			select {
			case <-t.C: // Every 100ms increate number of ticks and update UI
				ui.Eval(fmt.Sprintf(`document.querySelector('.timer').innerText = 0.1*%d`,
					atomic.AddUint32(&ticks, 1)))
			case <-togglec: // If paused - wait for another toggle event to unpause
				<-togglec
			}
		}
	}()
	<-ui.Done()
}
