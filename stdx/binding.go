package stdx

import (
	"github.com/go-playground/form/v4"
)

// One decoder per source, each reading its own struct tag, so a DTO can carry
// `query`, `form`, `uri` and `header` tags side by side — the shape generated
// handlers bind.
var (
	queryDecoder  = newDecoder("query")
	formDecoder   = newDecoder("form")
	uriDecoder    = newDecoder("uri")
	headerDecoder = newDecoder("header")
)

func newDecoder(tag string) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
	// An unknown key is data the caller sent, not a programming error: binding
	// must not fail because a client added a query parameter.
	decoder.SetMode(form.ModeImplicit)
	return decoder
}
