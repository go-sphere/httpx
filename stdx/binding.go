package stdx

import (
	"fmt"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"

	"github.com/go-playground/form/v4"
)

// One decoder per source, each reading its own struct tag, so a DTO can carry
// `query`, `form`, `uri` and `header` tags side by side — the shape generated
// handlers bind.
var (
	queryDecoder  = newDecoder("query")
	formDecoder   = newDecoder("form")
	uriDecoder    = newDecoder("uri")
	headerDecoder = newHeaderDecoder()
)

func newDecoder(tag string) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
	// An unknown key is data the caller sent, not a programming error: binding
	// must not fail because a client added a query parameter.
	decoder.SetMode(form.ModeImplicit)
	return decoder
}

func newHeaderDecoder() *form.Decoder {
	d := newDecoder("header")
	d.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, options, hasOptions := strings.Cut(f.Tag.Get("header"), ",")
		if name == "" {
			name = f.Name
		}
		name = textproto.CanonicalMIMEHeaderKey(name)
		if hasOptions {
			return name + "," + options
		}
		return name
	})
	return d
}

func validateFormKeys(values url.Values) error {
	for k := range values {
		insideBracket := false
		for i := 0; i < len(k); i++ {
			switch k[i] {
			case '[':
				if insideBracket {
					return fmt.Errorf("stdx: invalid form key %q: unexpected '['", k)
				}
				insideBracket = true
			case ']':
				if !insideBracket {
					return fmt.Errorf("stdx: invalid form key %q: missing '['", k)
				}
				insideBracket = false
			}
		}
		if insideBracket {
			return fmt.Errorf("stdx: invalid form key %q: missing ']'", k)
		}
	}
	return nil
}

func decodeFormValues(d *form.Decoder, dst any, values url.Values) (err error) {
	if err := validateFormKeys(values); err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("stdx: form decode panic: %v", r)
		}
	}()
	return d.Decode(dst, values)
}
