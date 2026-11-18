package stdx

import (
	"errors"
	"fmt"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"

	"github.com/go-playground/form/v4"
)

func checkBindTarget(dst any) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("stdx: bind target must be a non-nil pointer")
	}
	return nil
}

// One decoder per source, each reading its own struct tag, so a DTO can carry
// `query`, `form`, `uri` and `header` tags side by side — the shape generated
// handlers bind.
var (
	queryDecoder  = newExplicitDecoder("query", false)
	formDecoder   = newExplicitDecoder("form", false)
	uriDecoder    = newExplicitDecoder("uri", false)
	headerDecoder = newExplicitDecoder("header", true)
)

func newExplicitDecoder(tag string, canonicalHeader bool) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
	// Explicit tag lookup without falling back to Go field names prevents
	// cross-source overwriting (e.g. a json-only field filled from query/header),
	// while still recursing into embedded anonymous structs.
	decoder.RegisterTagNameFunc(func(f reflect.StructField) string {
		raw, ok := f.Tag.Lookup(tag)
		if !ok {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.Anonymous && ft.Kind() == reflect.Struct {
				return ""
			}
			return "-"
		}
		if raw == "-" || raw == "" {
			return "-"
		}
		name, opts, hasOpts := strings.Cut(raw, ",")
		if name == "-" {
			return "-"
		}
		if name == "" {
			name = f.Name
		}
		if canonicalHeader {
			name = textproto.CanonicalMIMEHeaderKey(name)
		}
		if hasOpts {
			return name + "," + opts
		}
		return name
	})
	return decoder
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
