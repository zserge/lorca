package lorca

import (
	"fmt"
	"io/ioutil"
	"os"
)

const (
	// PageA4Width is a width of an A4 page in pixels at 96dpi
	PageA4Width = 816
	// PageA4Height is a height of an A4 page in pixels at 96dpi
	PageA4Height = 1056
)

// PDF converts a given URL (may be a local file) to a PDF file. Script is
// evaluated before the page is printed to PDF, you may modify the contents of
// the page there of wait until the page is fully rendered. Width and height
// are page bounds in pixels. PDF by default uses 96dpi density. For A4 page
// you may use PageA4Width and PageA4Height constants.
func PDF(url, script string, width, height int) ([]byte, error) {
	return doHeadless(url, func(c *chrome) ([]byte, error) {
		if _, err := c.eval(script); err != nil {
			return nil, err
		}
		return c.pdf(width, height)
	})
}

// PNG converts a given URL (may be a local file) to a PNG image. Script is
// evaluated before the "screenshot" is taken, so you can modify the contents
// of a URL there. Image bounds are provides in pixels. Background is in ARGB
// format, the default value of zero keeps the background transparent. Scale
// allows zooming the page in and out.
//
// This function is most convenient to convert SVG to PNG of different sizes,
// for example when preparing Lorca app icons.
func PNG(url, script string, x, y, width, height int, bg uint32, scale float32) ([]byte, error) {
	return doHeadless(url, func(c *chrome) ([]byte, error) {
		if _, err := c.eval(script); err != nil {
			return nil, err
		}
		return c.png(x, y, width, height, bg, scale)
	})
}

func doHeadless(url string, f func(c *chrome) ([]byte, error)) ([]byte, error) {
	dir, err := ioutil.TempDir("", "lorca")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	args := append(defaultChromeArgs, fmt.Sprintf("--user-data-dir=%s", dir), "--remote-debugging-port=0", "--headless", url)
	chrome, err := newChromeWithArgs(ChromeExecutable(), args...)
	if err != nil {
		return nil, err
	}
	defer chrome.kill()
	return f(chrome)
}
