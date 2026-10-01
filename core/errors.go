package core

import "fmt"

// FieldError is an error that names the field it is about, as a JSON
// path or a parameter name, and the reason. Decoders and validators in
// this module return it (or a list of them) instead of a bare string, so
// that a refusal can be followed to the byte or field that caused it
// (LESSONS Z-02: each problem gives its path and a reason).
type FieldError struct {
	Field  string
	Reason string
}

// Error formats as "<field>: <reason>".
func (e *FieldError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}

// Fieldf builds a FieldError with a formatted reason.
func Fieldf(field, format string, args ...any) *FieldError {
	return &FieldError{Field: field, Reason: fmt.Sprintf(format, args...)}
}
