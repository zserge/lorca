package lorca

import "encoding/json"

// Value is a generic type of a JSON value (primitive, object, array) and
// optionally an error value.
type Value interface {
	Err() error
	To(interface{}) error
	Float() float32
	Int() int
	String() string
	Bool() bool
	Object() map[string]Value
	Array() []Value
}

type value struct {
	err error
	raw json.RawMessage
}

func (v value) Err() error             { return v.err }
func (v value) To(x interface{}) error { return json.Unmarshal(v.raw, x) }
func (v value) Float() (f float32)     { v.To(&f); return f }
func (v value) Int() (i int)           { v.To(&i); return i }
func (v value) String() (s string)     { v.To(&s); return s }
func (v value) Bool() (b bool)         { v.To(&b); return b }
func (v value) Array() (values []Value) {
	array := []json.RawMessage{}
	v.To(&array)
	for _, el := range array {
		values = append(values, value{raw: el})
	}
	return values
}
func (v value) Object() (object map[string]Value) {
	object = map[string]Value{}
	kv := map[string]json.RawMessage{}
	v.To(&kv)
	for k, v := range kv {
		object[k] = value{raw: v}
	}
	return object
}
