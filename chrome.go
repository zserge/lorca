package lorca

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"

	"golang.org/x/net/websocket"
)

type h = map[string]interface{}

type result struct {
	Value interface{}
	Err   error
}

type msg struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type chrome struct {
	sync.Mutex
	cmd      *exec.Cmd
	ws       *websocket.Conn
	id       int32
	session  string
	pending  map[int]chan result
	bindings map[string]func([]interface{}) (interface{}, error)
}

func newChromeWithArgs(chromeBinary string, args ...string) (*chrome, error) {
	// The first two IDs are used internally during the initialization
	c := &chrome{
		id:       2,
		pending:  map[int]chan result{},
		bindings: map[string]func([]interface{}) (interface{}, error){},
	}

	// Start chrome process
	c.cmd = exec.Command(chromeBinary, args...)
	pipe, err := c.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}

	// Wait for websocket address to be printed to stderr
	re := regexp.MustCompile(`^DevTools listening on (ws://.*)\n$`)
	m, err := readUntilMatch(pipe, re)
	if err != nil {
		c.kill()
		return nil, err
	}
	wsURL := m[1]

	// Open a websocket
	c.ws, err = websocket.Dial(wsURL, "", "http://127.0.0.1")
	if err != nil {
		c.kill()
		return nil, err
	}

	// Find target and initialize session
	target, err := c.findTarget()
	if err != nil {
		c.kill()
		return nil, err
	}

	c.session, err = c.startSession(target)
	if err != nil {
		c.kill()
		return nil, err
	}
	go c.readLoop()
	return c, nil
}

func (c *chrome) findTarget() (string, error) {
	err := websocket.JSON.Send(c.ws, h{
		"id": 0, "method": "Target.setDiscoverTargets", "params": h{"discover": true},
	})
	if err != nil {
		return "", err
	}
	for {
		m := msg{}
		if err = websocket.JSON.Receive(c.ws, &m); err != nil {
			return "", err
		} else if m.Method == "Target.targetCreated" {
			target := struct {
				TargetInfo struct {
					Type string `json:"type"`
					ID   string `json:"targetId"`
				} `json:"targetInfo"`
			}{}
			if err := json.Unmarshal(m.Params, &target); err != nil {
				return "", err
			} else if target.TargetInfo.Type == "page" {
				return target.TargetInfo.ID, nil
			}
		}
	}
}

func (c *chrome) startSession(target string) (string, error) {
	err := websocket.JSON.Send(c.ws, h{
		"id": 1, "method": "Target.attachToTarget", "params": h{"targetId": target},
	})
	if err != nil {
		return "", err
	}
	for {
		m := msg{}
		if err = websocket.JSON.Receive(c.ws, &m); err != nil {
			return "", err
		} else if m.ID == 1 {
			if m.Error != nil {
				return "", errors.New("Target error: " + string(m.Error))
			}
			session := struct {
				ID string `json:"sessionId"`
			}{}
			if err := json.Unmarshal(m.Result, &session); err != nil {
				return "", err
			}
			return session.ID, nil
		}
	}
}

func (c *chrome) readLoop() {
	for {
		m := msg{}
		if err := websocket.JSON.Receive(c.ws, &m); err != nil {
			return
		}
		if m.Method == "Target.receivedMessageFromTarget" {
			params := struct {
				SessionID string `json:"sessionId"`
				Message   string `json:"message"`
			}{}
			json.Unmarshal(m.Params, &params)
			if params.SessionID != c.session {
				continue
			}
			res := struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
				Params struct {
					Name    string `json:"name"`
					Payload string `json:"payload"`
					ID      int    `json:"executionContextId"`
				} `json:"params"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Result struct {
					Result struct {
						Type        string      `json:"type"`
						Subtype     string      `json:"subtype"`
						Description string      `json:"description"`
						Value       interface{} `json:"value"`
						ObjectID    string      `json:"objectId"`
					} `json:"result"`
				} `json:"result"`
			}{}
			json.Unmarshal([]byte(params.Message), &res)

			if res.ID == 0 && res.Method == "Runtime.bindingCalled" {
				payload := struct {
					Name string        `json:"name"`
					Seq  int           `json:"seq"`
					Args []interface{} `json:"args"`
				}{}
				json.Unmarshal([]byte(res.Params.Payload), &payload)

				c.Lock()
				binding, ok := c.bindings[res.Params.Name]
				c.Unlock()
				if ok {
					go func() {
						r, err := binding(payload.Args)
						b, err := json.Marshal(r)
						// TOOD: handle errors
						_ = err
						expr := fmt.Sprintf(`
						window['%[1]s']['callbacks'].get(%[2]d)('%[3]s');
						window['%[1]s']['callbacks'].delete(%[2]d);
						`, payload.Name, payload.Seq, string(b))
						c.Send("Runtime.evaluate", h{"expression": expr, "contextId": res.Params.ID})
					}()
				}
				continue
			}

			c.Lock()
			resc, ok := c.pending[res.ID]
			delete(c.pending, res.ID)
			c.Unlock()

			if !ok {
				continue
			}

			if res.Error.Message != "" {
				resc <- result{Err: errors.New(res.Error.Message)}
			} else if res.Result.Result.Type == "object" && res.Result.Result.Subtype == "error" {
				resc <- result{Err: errors.New(res.Result.Result.Description)}
			} else {
				resc <- result{Value: res.Result.Result.Value}
			}
		}
	}
}

func (c *chrome) Send(method string, params h) (interface{}, error) {
	id := atomic.AddInt32(&c.id, 1)
	b, err := json.Marshal(h{"id": int(id), "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	resc := make(chan result)
	c.Lock()
	c.pending[int(id)] = resc
	c.Unlock()

	if err := websocket.JSON.Send(c.ws, h{
		"id":     int(id),
		"method": "Target.sendMessageToTarget",
		"params": h{"message": string(b), "sessionId": c.session},
	}); err != nil {
		return nil, err
	}
	res := <-resc
	return res.Value, res.Err
}

func (c *chrome) bind(name string, f func([]interface{}) (interface{}, error)) error {
	c.Lock()
	c.bindings[name] = f
	c.Unlock()
	if _, err := c.Send("Runtime.addBinding", h{"name": name}); err != nil {
		return err
	}
	script := fmt.Sprintf(`
	const bindingName = '%s';
	const binding = window[bindingName];
	window[bindingName] = async (...args) => {
		const me = window[bindingName];
		let callbacks = me['callbacks'];
		if (!callbacks) {
			callbacks = new Map();
			me['callbacks'] = callbacks;
		}
		const seq = (me['lastSeq'] || 0) + 1;
		me['lastSeq'] = seq;
		const promise = new Promise(fulfill => callbacks.set(seq, fulfill));
		binding(JSON.stringify({name: bindingName, seq, args}));
		return promise;
	};
	`, name)
	_, err := c.Send("Page.addScriptToEvaluateOnNewDocument", h{"source": script})
	return err
}

func (c *chrome) kill() error {
	if c.ws != nil {
		if err := c.ws.Close(); err != nil {
			return err
		}
	}
	return c.cmd.Process.Kill()
}

func readUntilMatch(r io.ReadCloser, re *regexp.Regexp) ([]string, error) {
	br := bufio.NewReader(r)
	for {
		if line, err := br.ReadString('\n'); err != nil {
			r.Close()
			return nil, err
		} else if m := re.FindStringSubmatch(line); m != nil {
			go io.Copy(ioutil.Discard, br)
			return m, nil
		}
	}
}
