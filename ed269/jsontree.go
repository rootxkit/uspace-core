package ed269

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
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
// order; numbers keep their text, so nothing is lost before the
// validators decide what a field may hold.
type value struct {
	kind kind
	b    bool
	s    string // a string, or a number's text
	arr  []*value
	keys []string // object member names, parallel to vals
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

// present reports whether v is given and not null. ED-269 reads a null
// optional field as absent (LESSONS Z-01).
func present(v *value) bool { return v != nil && v.kind != kindNull }

// describe names v's JSON type for a reason ("must be a string, not a
// number").
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

// float returns a number's value; false when v is not a number or the
// number is not finite (1e400).
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

// frame is one open object or array while decoding.
type frame struct {
	v      *value
	key    string // the pending member name of an object
	hasKey bool
	seen   map[string]struct{} // member names, once the object is large
}

// seenLinear is the member count below which duplicate names are found by
// a linear scan rather than a map.
const seenLinear = 8

// decodeTree reads data as one JSON value without recursion, refusing
// nesting deeper than maxDepth and repeated member names. root is the
// path of the value for problems (empty for a document). It returns nil
// when data is not JSON or too deep; repeated names are problems but the
// tree is still returned so that the rest can be checked.
func decodeTree(data []byte, maxDepth int, root string, ps *collector) *value {
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
				ps.add(root, "not JSON: unexpected end of input")
				return nil
			}
			return top
		}
		if err != nil {
			ps.add(root, "not JSON: "+err.Error())
			return nil
		}
		if top != nil && len(stack) == 0 {
			ps.add(root, "not JSON: data after the end of the document")
			return nil
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if len(stack) >= maxDepth {
					ps.add(root, "nested too deeply to be ED-269")
					return nil
				}
				v := &value{kind: kindArray}
				if t == '{' {
					v.kind = kindObject
				}
				attach(v)
				stack = append(stack, frame{v: v})
			default: // '}' or ']'
				stack = stack[:len(stack)-1]
			}
		case string:
			f := topFrame(stack)
			if f != nil && f.v.kind == kindObject && !f.hasKey {
				if f.repeated(t) {
					ps.add(join(pathOf(root, stack), t), "repeated member name; a name may appear once")
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

func topFrame(stack []frame) *frame {
	if len(stack) == 0 {
		return nil
	}
	return &stack[len(stack)-1]
}

// repeated records name in the object and reports whether it was already
// there.
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

// pathOf builds the path of the innermost open container, for the rare
// problem found while decoding.
func pathOf(root string, stack []frame) string {
	p := root
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

// compact writes v back as compact JSON, member order and number text as
// read.
func compact(v *value, b *bytes.Buffer) { compactUpTo(v, b, 0) }

// compactUpTo is compact that stops writing once b holds at least limit
// bytes (0: no limit), so that a reason quoting a large subtree costs
// no more than its cap.
func compactUpTo(v *value, b *bytes.Buffer, limit int) {
	if limit > 0 && b.Len() >= limit {
		return
	}
	switch v.kind {
	case kindNull:
		b.WriteString("null")
	case kindBool:
		b.WriteString(strconv.FormatBool(v.b))
	case kindNumber:
		b.WriteString(capText(v.s, limit))
	case kindString:
		writeString(b, capText(v.s, limit))
	case kindArray:
		b.WriteByte('[')
		for i, e := range v.arr {
			if i > 0 {
				b.WriteByte(',')
			}
			compactUpTo(e, b, limit)
		}
		b.WriteByte(']')
	case kindObject:
		b.WriteByte('{')
		for i, k := range v.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, capText(k, limit))
			b.WriteByte(':')
			compactUpTo(v.vals[i], b, limit)
		}
		b.WriteByte('}')
	}
}

// capText cuts s to at most limit bytes on a character boundary (0: no
// limit).
func capText(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	cut := 0
	for i := range s {
		if i > limit {
			break
		}
		cut = i
	}
	return s[:cut]
}

// writeString writes s as a JSON string without HTML escaping.
func writeString(b *bytes.Buffer, s string) {
	var sb bytes.Buffer
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	// Encoding a string cannot fail.
	_ = enc.Encode(s)
	b.WriteString(strings.TrimSuffix(sb.String(), "\n"))
}
