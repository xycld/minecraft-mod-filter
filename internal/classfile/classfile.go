// Package classfile reads the parts of a JVM class file that side detection
// needs: the class name, referenced classes and class-level annotations.
package classfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Class is a parsed class file.
type Class struct {
	Name        string
	Super       string
	Interfaces  []string
	Refs        []string // every class referenced from the constant pool, internal names
	Annotations []Annotation
}

// Annotation is a class-level annotation. Type is the internal class name.
type Annotation struct {
	Type   string
	Values map[string]any
}

// Enum is an enum constant annotation value.
type Enum struct {
	Type string // internal class name
	Name string
}

// ClassValue is a class literal annotation value (internal name or descriptor).
type ClassValue string

// Find returns the first annotation of the given internal type name.
func (c *Class) Find(typ string) (Annotation, bool) {
	for _, a := range c.Annotations {
		if a.Type == typ {
			return a, true
		}
	}
	return Annotation{}, false
}

// HasRefPrefix reports whether any referenced class starts with prefix.
func (c *Class) HasRefPrefix(prefix string) bool {
	for _, r := range c.Refs {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

var ErrNotClass = errors.New("not a class file")

const (
	tagUtf8               = 1
	tagInteger            = 3
	tagFloat              = 4
	tagLong               = 5
	tagDouble             = 6
	tagClass              = 7
	tagString             = 8
	tagFieldref           = 9
	tagMethodref          = 10
	tagInterfaceMethodref = 11
	tagNameAndType        = 12
	tagMethodHandle       = 15
	tagMethodType         = 16
	tagDynamic            = 17
	tagInvokeDynamic      = 18
	tagModule             = 19
	tagPackage            = 20
)

type cpEntry struct {
	tag  byte
	a, b uint16
	str  string
	num  uint64
}

type reader struct {
	buf []byte
	off int
	err error
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.off+n > len(r.buf) {
		r.err = fmt.Errorf("classfile truncated at offset %d", r.off)
		return false
	}
	return true
}

func (r *reader) u1() byte {
	if !r.need(1) {
		return 0
	}
	r.off++
	return r.buf[r.off-1]
}

func (r *reader) u2() uint16 {
	if !r.need(2) {
		return 0
	}
	r.off += 2
	return binary.BigEndian.Uint16(r.buf[r.off-2:])
}

func (r *reader) u4() uint32 {
	if !r.need(4) {
		return 0
	}
	r.off += 4
	return binary.BigEndian.Uint32(r.buf[r.off-4:])
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	r.off += n
	return r.buf[r.off-n : r.off]
}

// Parse parses a class file.
func Parse(data []byte) (*Class, error) {
	r := &reader{buf: data}
	if r.u4() != 0xCAFEBABE {
		return nil, ErrNotClass
	}
	r.u2() // minor
	r.u2() // major
	cp := make([]cpEntry, r.u2())
	for i := 1; i < len(cp) && r.err == nil; i++ {
		e := cpEntry{tag: r.u1()}
		switch e.tag {
		case tagUtf8:
			e.str = string(r.bytes(int(r.u2())))
		case tagInteger, tagFloat:
			e.num = uint64(r.u4())
		case tagLong, tagDouble:
			e.num = uint64(r.u4())<<32 | uint64(r.u4())
			cp[i] = e
			i++ // 8-byte constants take two slots
			continue
		case tagClass, tagString, tagMethodType, tagModule, tagPackage:
			e.a = r.u2()
		case tagFieldref, tagMethodref, tagInterfaceMethodref, tagNameAndType, tagDynamic, tagInvokeDynamic:
			e.a, e.b = r.u2(), r.u2()
		case tagMethodHandle:
			r.u1()
			e.a = r.u2()
		default:
			return nil, fmt.Errorf("unknown constant pool tag %d", e.tag)
		}
		cp[i] = e
	}
	if r.err != nil {
		return nil, r.err
	}
	p := &parser{r: r, cp: cp}

	c := &Class{}
	r.u2() // access flags
	c.Name = p.className(r.u2())
	c.Super = p.className(r.u2())
	for n := r.u2(); n > 0 && r.err == nil; n-- {
		c.Interfaces = append(c.Interfaces, p.className(r.u2()))
	}
	for members := 0; members < 2; members++ { // fields, then methods
		for n := r.u2(); n > 0 && r.err == nil; n-- {
			r.bytes(6) // access, name, descriptor
			for a := r.u2(); a > 0 && r.err == nil; a-- {
				r.u2()
				r.bytes(int(r.u4()))
			}
		}
	}
	for n := r.u2(); n > 0 && r.err == nil; n-- {
		name := p.utf8(r.u2())
		body := r.bytes(int(r.u4()))
		if name == "RuntimeVisibleAnnotations" || name == "RuntimeInvisibleAnnotations" {
			sub := &parser{r: &reader{buf: body}, cp: cp}
			anns := sub.annotations()
			if sub.r.err == nil {
				c.Annotations = append(c.Annotations, anns...)
			}
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	c.Refs = collectRefs(cp)
	return c, nil
}

// maxAnnotationDepth bounds nested annotations/arrays; real ones nest a few
// levels, hostile class files could otherwise exhaust the stack.
const maxAnnotationDepth = 32

type parser struct {
	r     *reader
	cp    []cpEntry
	depth int
}

func (p *parser) entry(i uint16) *cpEntry {
	if int(i) >= len(p.cp) {
		if p.r.err == nil {
			p.r.err = fmt.Errorf("constant pool index %d out of range", i)
		}
		return &cpEntry{}
	}
	return &p.cp[i]
}

func (p *parser) utf8(i uint16) string { return p.entry(i).str }

func (p *parser) className(i uint16) string {
	if i == 0 {
		return ""
	}
	return p.utf8(p.entry(i).a)
}

func (p *parser) annotations() []Annotation {
	var out []Annotation
	for n := p.r.u2(); n > 0 && p.r.err == nil; n-- {
		out = append(out, p.annotation())
	}
	return out
}

func (p *parser) annotation() Annotation {
	if !p.enter() {
		return Annotation{}
	}
	defer p.leave()
	a := Annotation{Type: descriptorName(p.utf8(p.r.u2())), Values: map[string]any{}}
	for n := p.r.u2(); n > 0 && p.r.err == nil; n-- {
		name := p.utf8(p.r.u2())
		a.Values[name] = p.elementValue()
	}
	return a
}

func (p *parser) enter() bool {
	p.depth++
	if p.depth > maxAnnotationDepth && p.r.err == nil {
		p.r.err = fmt.Errorf("annotations nested deeper than %d", maxAnnotationDepth)
	}
	return p.r.err == nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) elementValue() any {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	switch tag := p.r.u1(); tag {
	case 's':
		return p.utf8(p.r.u2())
	case 'Z':
		return p.entry(p.r.u2()).num != 0
	case 'B', 'C', 'I', 'S':
		return int64(int32(p.entry(p.r.u2()).num))
	case 'J':
		return int64(p.entry(p.r.u2()).num)
	case 'F':
		return float64(math.Float32frombits(uint32(p.entry(p.r.u2()).num)))
	case 'D':
		return math.Float64frombits(p.entry(p.r.u2()).num)
	case 'e':
		typ := descriptorName(p.utf8(p.r.u2()))
		return Enum{Type: typ, Name: p.utf8(p.r.u2())}
	case 'c':
		return ClassValue(descriptorName(p.utf8(p.r.u2())))
	case '@':
		return p.annotation()
	case '[':
		n := p.r.u2()
		// Each element takes at least 3 bytes; never trust the count alone.
		vals := make([]any, 0, min(int(n), (len(p.r.buf)-p.r.off)/3))
		for ; n > 0 && p.r.err == nil; n-- {
			vals = append(vals, p.elementValue())
		}
		return vals
	default:
		if p.r.err == nil {
			p.r.err = fmt.Errorf("unknown element value tag %q", tag)
		}
		return nil
	}
}

// descriptorName turns "Lfoo/Bar;" into "foo/Bar"; other strings pass through.
func descriptorName(d string) string {
	if len(d) > 2 && d[0] == 'L' && d[len(d)-1] == ';' {
		return d[1 : len(d)-1]
	}
	return d
}

// collectRefs gathers class names from Class constants and from every
// descriptor-looking "Lpkg/Name;" sequence in Utf8 constants.
func collectRefs(cp []cpEntry) []string {
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimLeft(name, "[")
		name = descriptorName(name)
		if name != "" && !seen[name] {
			seen[name] = true
		}
	}
	for _, e := range cp {
		switch e.tag {
		case tagClass:
			if int(e.a) < len(cp) {
				add(cp[e.a].str)
			}
		case tagUtf8:
			descriptorRefs(e.str, add)
		}
	}
	refs := make([]string, 0, len(seen))
	for name := range seen {
		refs = append(refs, name)
	}
	return refs
}

// descriptorRefs calls add for every "Lpkg/Name;" (or "Lpkg/Name<") in s in
// a single linear pass.
func descriptorRefs(s string, add func(string)) {
	for i := 0; i < len(s); i++ {
		if s[i] != 'L' {
			continue
		}
		j := i + 1
		for j < len(s) && nameByte(s[j]) {
			j++
		}
		if j < len(s) && (s[j] == ';' || s[j] == '<') {
			if name := s[i+1 : j]; strings.IndexByte(name, '/') > 0 {
				add(name)
			}
		}
		i = j - 1 // never rescan a run, keeping the pass linear
	}
}

func nameByte(c byte) bool {
	return c == '/' || c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
