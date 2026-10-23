// Package testjar builds synthetic mod jars and class files for tests, so the
// test suite needs no third-party binaries.
package testjar

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"sort"
)

// Jar is an in-memory jar under construction.
type Jar struct {
	files map[string][]byte
}

func New() *Jar { return &Jar{files: map[string][]byte{}} }

func (j *Jar) File(name, content string) *Jar {
	j.files[name] = []byte(content)
	return j
}

func (j *Jar) Raw(name string, data []byte) *Jar {
	j.files[name] = data
	return j
}

// Class adds a class file built from c.
func (j *Jar) Class(c Class) *Jar {
	j.files[c.Name+".class"] = c.Bytes()
	return j
}

// Bytes returns the zip-encoded jar.
func (j *Jar) Bytes() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(j.files))
	for n := range j.files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			panic(err)
		}
		w.Write(j.files[n])
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// Class describes a class file to generate.
type Class struct {
	Name        string
	Refs        []string // referenced classes (internal names)
	Annotations []Annotation
}

type Annotation struct {
	Type   string // internal name, e.g. "net/neoforged/fml/common/Mod"
	Values map[string]any
}

// Enum is an enum constant annotation value.
type Enum struct{ Type, Name string }

// ClassRef is a class literal annotation value.
type ClassRef string

type pool struct {
	buf   bytes.Buffer
	count uint16
	utf8  map[string]uint16
	class map[string]uint16
	ints  map[int32]uint16
}

func (p *pool) u1(v byte)   { p.buf.WriteByte(v) }
func (p *pool) u2(v uint16) { binary.Write(&p.buf, binary.BigEndian, v) }

func (p *pool) Utf8(s string) uint16 {
	if i, ok := p.utf8[s]; ok {
		return i
	}
	p.count++
	p.u1(1)
	p.u2(uint16(len(s)))
	p.buf.WriteString(s)
	p.utf8[s] = p.count
	return p.count
}

func (p *pool) Class(name string) uint16 {
	if i, ok := p.class[name]; ok {
		return i
	}
	n := p.Utf8(name)
	p.count++
	p.u1(7)
	p.u2(n)
	p.class[name] = p.count
	return p.count
}

func (p *pool) Int(v int32) uint16 {
	if i, ok := p.ints[v]; ok {
		return i
	}
	p.count++
	p.u1(3)
	binary.Write(&p.buf, binary.BigEndian, v)
	p.ints[v] = p.count
	return p.count
}

// Bytes encodes the class file.
func (c Class) Bytes() []byte {
	p := &pool{utf8: map[string]uint16{}, class: map[string]uint16{}, ints: map[int32]uint16{}}
	this := p.Class(c.Name)
	super := p.Class("java/lang/Object")
	for _, r := range c.Refs {
		p.Class(r)
	}
	var ann bytes.Buffer
	w2 := func(v uint16) { binary.Write(&ann, binary.BigEndian, v) }
	var value func(v any)
	value = func(v any) {
		switch v := v.(type) {
		case string:
			ann.WriteByte('s')
			w2(p.Utf8(v))
		case bool:
			ann.WriteByte('Z')
			b := int32(0)
			if v {
				b = 1
			}
			w2(p.Int(b))
		case int:
			ann.WriteByte('I')
			w2(p.Int(int32(v)))
		case Enum:
			ann.WriteByte('e')
			w2(p.Utf8("L" + v.Type + ";"))
			w2(p.Utf8(v.Name))
		case ClassRef:
			ann.WriteByte('c')
			w2(p.Utf8("L" + string(v) + ";"))
		case []any:
			ann.WriteByte('[')
			w2(uint16(len(v)))
			for _, e := range v {
				value(e)
			}
		default:
			panic("testjar: unsupported annotation value")
		}
	}
	w2(uint16(len(c.Annotations)))
	for _, a := range c.Annotations {
		w2(p.Utf8("L" + a.Type + ";"))
		keys := make([]string, 0, len(a.Values))
		for k := range a.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w2(uint16(len(keys)))
		for _, k := range keys {
			w2(p.Utf8(k))
			value(a.Values[k])
		}
	}
	attrName := p.Utf8("RuntimeVisibleAnnotations")

	var out bytes.Buffer
	w := func(v any) { binary.Write(&out, binary.BigEndian, v) }
	w(uint32(0xCAFEBABE))
	w(uint16(0))
	w(uint16(52))
	w(p.count + 1)
	out.Write(p.buf.Bytes())
	w(uint16(0x21)) // public super
	w(this)
	w(super)
	w(uint16(0)) // interfaces
	w(uint16(0)) // fields
	w(uint16(0)) // methods
	if len(c.Annotations) == 0 {
		w(uint16(0))
	} else {
		w(uint16(1))
		w(attrName)
		w(uint32(ann.Len()))
		out.Write(ann.Bytes())
	}
	return out.Bytes()
}
