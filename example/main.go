package main

import (
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"

	"github.com/zserge/lorca"
)

const html = `
<!doctype html>
<html>
	<head>
		<title>Counter</title>
		<style>
		* { margin: 0; padding: 0; box-sizing: border-box; user-select: none; }
		body { height: 100vh; display: flex; align-items: center; justify-content: center; }
		.counter-container { display: flex; flex-direction: column; align-items: center; }
		.btn-row { display: flex; align-items: center; }
		.btn { cursor: pointer; min-width: 4em; border: 1px solid black; text-align: center; margin: 0.5rem; }
		</style>
	</head>
	<body onload=start()>

		<!-- UI layout -->
		<div class="counter-container">
			<div class="counter"></div>
			<div class="btn-row">
				<div class="btn btn-incr">+1</div>
				<div class="btn btn-decr">-1</div>
			</div>
		</div>

		<!-- Connect UI actions to Go functions -->
		<script type="application/javascript">
			const counter = document.querySelector('.counter');
			const btnIncr = document.querySelector('.btn-incr');
			const btnDecr = document.querySelector('.btn-decr');

			// We use async/await because Go functions are asynchronous
			const render = async () => {
				counter.innerText = ` + "`Count: ${await counterValue()}`" + `;
			};

			btnIncr.addEventListener('click', async () => {
				await counterAdd(1); // Call Go function
				render();
			});

			btnDecr.addEventListener('click', async () => {
				await counterAdd(-1); // Call Go function
				render();
			});

			render();
		</script>
	</body>
</html>
`

// Go types that are bound to the UI must be thread-safe, because each binding
// is executed in its own goroutine. In this simple case we may use atomic
// operations, but for more complex cases one should use proper synchronization.
type counter struct {
	sync.Mutex
	count int
}

func (c *counter) Add(n int) {
	c.Lock()
	defer c.Unlock()
	c.count = c.count + n
}

func (c *counter) Value() int {
	c.Lock()
	defer c.Unlock()
	return c.count
}

func main() {
	ui, err := lorca.New("", "", 480, 320)
	if err != nil {
		log.Fatal(err)
	}
	defer ui.Close()

	// A simple way to know when UI is ready (uses body.onload even in JS)
	ui.Bind("start", func() {
		log.Println("UI is ready")
	})

	// Create and bind Go object to the UI
	c := &counter{}
	ui.Bind("counterAdd", c.Add)
	ui.Bind("counterValue", c.Value)

	// Load HTML, a local web server on a random port would also work
	ui.Load("data:text/html," + url.PathEscape(html))

	// You may use console.log to debug your JS code, it will be printed via
	// log.Println(). Also exceptions are printed in a similar manner.
	ui.Eval(`
		console.log("Hello, world!");
		console.log('Multiple values:', [1, false, {"x":5}]);
	`)

	// Wait until the interrupt signal arrives or browser window is closed
	sigc := make(chan os.Signal)
	signal.Notify(sigc, os.Interrupt)
	select {
	case <-sigc:
	case <-ui.Done():
	}

	log.Println("exiting...")
}
