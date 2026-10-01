package ed318

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rootxkit/uspace-core/ed269"
)

// kind is the JSON type of a value.
type kind uint8

const (
	kindNull kind = iota
	kindBool
	kindNumber
	kindString
	kindArray
	kindObject
)

// value is one decoded JSON value. Objects keep their members in input
// order and numbers keep their text, so that nothing is lost before the
// validators decide what a member may hold, and members the schema allows
// but this package does not interpret are written back as read.
type value struct {
	kind kind
	b    bool
	s    string // a string, or a number's text
	arr  []*value
	keys []string
	vals []*value
}

// get returns the member key of an object, or nil when absent.
func (v *value) get(key string) *value {
	for i, k := range v.keys {
		if k == key {
			return v.vals[i]
		}
	}
	return nil
}

// present reports whether v is given and not null. A null optional member
// is read as absent and written back absent (as ed269 does).
func present(v *value) bool { return v != nil && v.kind != kindNull }

// describe names v's JSON type for a reason.
func describe(v *value) string {
	if v == nil {
		return "null"
	}
	switch v.kind {
	case kindNull:
		return "null"
	case kindBool:
		return "a boolean"
	case kindNumber:
		return "a number"
	case kindString:
		return "a string"
	case kindArray:
		return "a list"
	case kindObject:
		return "an object"
	}
	return "unknown"
}

// float returns a number's value; false when v is not a finite number.
func (v *value) float() (float64, bool) {
	if v == nil || v.kind != kindNumber {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// raw is v as compact JSON, member order and number text as read.
func (v *value) raw() json.RawMessage {
	var b bytes.Buffer
	compact(v, &b, 0)
	return json.RawMessage(b.Bytes())
}

type frame struct {
	v      *value
	key    string
	hasKey bool
	seen   map[string]struct{}
}

// seenLinear is the member count below which repeated names are found by
// a linear scan.
const seenLinear = 8

// decodeTree reads data as one JSON value without recursion, refusing
// nesting deeper than maxDepth and repeated member names. It returns nil
// when data is not one JSON value or nests too deeply.
func decodeTree(data []byte, maxDepth int, ps *collector) *value {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var top *value
	var stack []frame
	attach := func(v *value) {
		if len(stack) == 0 {
			top = v
			return
		}
		f := &stack[len(stack)-1]
		if f.v.kind == kindArray {
			f.v.arr = append(f.v.arr, v)
			return
		}
		f.v.keys = append(f.v.keys, f.key)
		f.v.vals = append(f.v.vals, v)
		f.hasKey = false
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if top == nil || len(stack) > 0 {
				ps.add("$", "not JSON: unexpected end of input")
				return nil
			}
			return top
		}
		if err != nil {
			ps.add("$", "not JSON: "+err.Error())
			return nil
		}
		if top != nil && len(stack) == 0 {
			ps.add("$", "not JSON: data after the end of the document")
			return nil
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if len(stack) >= maxDepth {
					ps.add("$", "nested too deeply to be ED-318")
					return nil
				}
				v := &value{kind: kindArray}
				if t == '{' {
					v.kind = kindObject
				}
				attach(v)
				stack = append(stack, frame{v: v})
			default:
				stack = stack[:len(stack)-1]
			}
		case string:
			if n := len(stack); n > 0 && stack[n-1].v.kind == kindObject && !stack[n-1].hasKey {
				f := &stack[n-1]
				if f.repeated(t) {
					ps.add(join(pathOf(stack), t), "repeated member name; a name may appear once")
				}
				f.key, f.hasKey = t, true
				continue
			}
			attach(&value{kind: kindString, s: t})
		case json.Number:
			attach(&value{kind: kindNumber, s: string(t)})
		case bool:
			attach(&value{kind: kindBool, b: t})
		case nil:
			attach(&value{kind: kindNull})
		}
	}
}

// repeated records name in the object and reports whether it was there.
func (f *frame) repeated(name string) bool {
	if f.seen == nil {
		for _, k := range f.v.keys {
			if k == name {
				return true
			}
		}
		if len(f.v.keys) < seenLinear {
			return false
		}
		f.seen = make(map[string]struct{}, 2*len(f.v.keys))
		for _, k := range f.v.keys {
			f.seen[k] = struct{}{}
		}
	}
	if _, ok := f.seen[name]; ok {
		return true
	}
	f.seen[name] = struct{}{}
	return false
}

// pathOf is the path of the innermost open container.
func pathOf(stack []frame) string {
	p := ""
	for i := 1; i < len(stack); i++ {
		parent := stack[i-1]
		if parent.v.kind == kindArray {
			p = index(p, len(parent.v.arr)-1)
		} else {
			p = join(p, parent.key)
		}
	}
	return p
}

// compact writes v as compact JSON, stopping once b holds limit bytes (0:
// no limit).
func compact(v *value, b *bytes.Buffer, limit int) {
	if limit > 0 && b.Len() >= limit {
		return
	}
	switch v.kind {
	case kindNull:
		b.WriteString("null")
	case kindBool:
		b.WriteString(strconv.FormatBool(v.b))
	case kindNumber:
		b.WriteString(v.s)
	case kindString:
		writeString(b, v.s)
	case kindArray:
		b.WriteByte('[')
		for i, e := range v.arr {
			if i > 0 {
				b.WriteByte(',')
			}
			compact(e, b, limit)
		}
		b.WriteByte(']')
	case kindObject:
		b.WriteByte('{')
		for i, k := range v.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			compact(v.vals[i], b, limit)
		}
		b.WriteByte('}')
	}
}

// writeString writes s as a JSON string without HTML escaping.
func writeString(b *bytes.Buffer, s string) {
	var sb bytes.Buffer
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // encoding a string cannot fail
	b.WriteString(strings.TrimSuffix(sb.String(), "\n"))
}

// collector gathers problems up to a cap.
type collector struct {
	list []ed269.Problem
	more int
	max  int
}

func (c *collector) add(field, reason string) {
	if field == "" {
		field = "$"
	}
	if len(c.list) < c.max {
		c.list = append(c.list, ed269.Problem{Field: field, Reason: reason})
		return
	}
	c.more++
}

func (c *collector) count() int { return len(c.list) + c.more }

func (c *collector) result() *ed269.Problems {
	if c.count() == 0 {
		return nil
	}
	return &ed269.Problems{List: c.list, Truncated: c.more}
}

// join builds the path of a member of the object at parent.
func join(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// index builds the path of an element of the list at parent.
func index(parent string, i int) string {
	if parent == "" {
		parent = "$"
	}
	return parent + "[" + strconv.Itoa(i) + "]"
}

// reasonValueMax is the most characters of a value a reason quotes.
const reasonValueMax = 64

// quote writes a string as the reasons show it: 'value', clipped.
func quote(s string) string {
	n := 0
	for i := range s {
		if n == reasonValueMax {
			return "'" + s[:i] + "…'"
		}
		n++
	}
	return "'" + s + "'"
}

// show writes a value as the reasons show it.
func show(v *value) string {
	if v == nil {
		return "null"
	}
	if v.kind == kindString {
		return quote(v.s)
	}
	var b bytes.Buffer
	compact(v, &b, 4*reasonValueMax)
	s := b.String()
	if utf8.RuneCountInString(s) > reasonValueMax {
		return quote(s)
	}
	return s
}
