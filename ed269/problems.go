package ed269

import (
	"strconv"
	"strings"
)

// Problem is one reason a document, zone or field was refused. Field is a
// JSON path into the input (`features[3].geometry[0].upperLimit`), or `$`
// for the document as a whole (LESSONS Z-02).
type Problem struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Problems is every problem found in one input, capped at
// Limits.MaxProblems; Truncated counts the problems beyond the cap. A nil
// *Problems means the input was accepted.
//
// *Problems implements error, but never assign one to an error variable
// or return it as error without a nil check: a nil *Problems stored in an
// error is a non-nil error (Go's typed nil), and the caller would read an
// accepted document as refused. Write
//
//	if probs != nil {
//		return probs
//	}
//	return nil
type Problems struct { //nolint:errname // docs/PLAN.md section 3.7 names it Problems; it is a report first and an error second
	List      []Problem
	Truncated int
}

// Error lists the problems as "field: reason; ...", with the count of the
// rest when the list was truncated.
func (p *Problems) Error() string {
	if p == nil {
		return "no problems"
	}
	var b strings.Builder
	b.WriteString("ed269: refused: ")
	for i, pr := range p.List {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(pr.Field)
		b.WriteString(": ")
		b.WriteString(pr.Reason)
	}
	if p.Truncated > 0 {
		b.WriteString("; and ")
		b.WriteString(strconv.Itoa(p.Truncated))
		b.WriteString(" more")
	}
	return b.String()
}

// Limits bounds what one import may cost (LESSONS Z-06, E-10). A zero
// field takes its DefaultLimits value.
type Limits struct {
	// IdentifierMax, NameMax and MessageMax are string lengths in
	// characters (Unicode code points), from uas_standards.
	IdentifierMax int
	NameMax       int
	MessageMax    int
	// OtherReasonMax, USpaceClassMax and AuthorityTextMax are the other
	// uas_standards lengths (otherReasonInfo; uSpaceClass; an authority's
	// name, service, contactName and phone).
	OtherReasonMax   int
	USpaceClassMax   int
	AuthorityTextMax int
	// ReasonsMax is the most entries in a zone's reason list.
	ReasonsMax int
	// MaxRingVertices is the most positions in one polygon ring. A
	// published zone has tens to hundreds (Luxembourg's largest 1,400).
	MaxRingVertices int
	// MaxProblems caps the report; the rest are counted in Truncated.
	MaxProblems int
	// MaxDepth is the deepest nesting of objects and arrays accepted. A
	// document nests 9 levels down to a position.
	MaxDepth int
	// MaxBytes is the largest input read; a larger one is refused before
	// it is parsed. The costliest input per byte is a list of one-digit
	// numbers: it allocates about 80 bytes per input byte (pinned by
	// TestMemoryPerInputByte) and was measured at about 250 bytes of
	// process memory per input byte (1 MiB took 150 ms). The 4 MiB
	// default therefore bounds a hostile file at about 1 GB; raise it
	// only with that cost in mind.
	MaxBytes int
}

// DefaultLimits are the limits of the vectors' header
// (ed269_parse.json `limits`) plus the depth and size bounds.
var DefaultLimits = Limits{
	IdentifierMax:    7,
	NameMax:          200,
	MessageMax:       200,
	OtherReasonMax:   30,
	USpaceClassMax:   100,
	AuthorityTextMax: 200,
	ReasonsMax:       9,
	MaxRingVertices:  5000,
	MaxProblems:      100,
	MaxDepth:         32,
	MaxBytes:         4 << 20,
}

// withDefaults fills every zero or negative field from DefaultLimits.
func (l Limits) withDefaults() Limits {
	fill := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	d := DefaultLimits
	fill(&l.IdentifierMax, d.IdentifierMax)
	fill(&l.NameMax, d.NameMax)
	fill(&l.MessageMax, d.MessageMax)
	fill(&l.OtherReasonMax, d.OtherReasonMax)
	fill(&l.USpaceClassMax, d.USpaceClassMax)
	fill(&l.AuthorityTextMax, d.AuthorityTextMax)
	fill(&l.ReasonsMax, d.ReasonsMax)
	fill(&l.MaxRingVertices, d.MaxRingVertices)
	fill(&l.MaxProblems, d.MaxProblems)
	fill(&l.MaxDepth, d.MaxDepth)
	fill(&l.MaxBytes, d.MaxBytes)
	return l
}

// collector gathers problems up to a cap.
type collector struct {
	list []Problem
	more int
	max  int
}

func (c *collector) add(field, reason string) {
	if field == "" {
		field = "$"
	}
	if len(c.list) < c.max {
		c.list = append(c.list, Problem{Field: field, Reason: reason})
		return
	}
	c.more++
}

// count is every problem found, listed or not.
func (c *collector) count() int { return len(c.list) + c.more }

// result is nil when nothing was found.
func (c *collector) result() *Problems {
	if c.count() == 0 {
		return nil
	}
	return &Problems{List: c.list, Truncated: c.more}
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
