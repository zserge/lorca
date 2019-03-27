package lorca

import (
	"encoding/json"
	"errors"
	"testing"
)

var errTest = errors.New("fail")

func TestValueError(t *testing.T) {
	v := value{err: errTest}
	if v.Err() != errTest {
		t.Fail()
	}

	v = value{raw: json.RawMessage(`"hello"`)}
	if v.Err() != nil {
		t.Fail()
	}
}

func TestValuePrimitive(t *testing.T) {
	v := value{raw: json.RawMessage(`42`)}
	if v.Int() != 42 {
		t.Fail()
	}
	v = value{raw: json.RawMessage(`"hello"`)}
	if v.Int() != 0 || v.String() != "hello" {
		t.Fail()
	}
	v = value{err: errTest}
	if v.Int() != 0 || v.String() != "" {
		t.Fail()
	}
}

func TestValueComplex(t *testing.T) {
	v := value{raw: json.RawMessage(`["foo", 42.3, {"x": 5}]`)}
	if len(v.Array()) != 3 {
		t.Fail()
	}
	if v.Array()[0].String() != "foo" {
		t.Fail()
	}
	if v.Array()[1].Float() != 42.3 {
		t.Fail()
	}
	if v.Array()[2].Object()["x"].Int() != 5 {
		t.Fail()
	}
}
