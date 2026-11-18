package hertzx

import (
	"fmt"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"

	"github.com/go-playground/form/v4"
)

var (
	formDecoder   = newExplicitDecoder("form", false)
	queryDecoder  = newExplicitDecoder("query", false)
	headerDecoder = newExplicitDecoder("header", true)
)

func newExplicitDecoder(tag string, canonicalHeader bool) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
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

func (c *hertzContext) bindQueryValues(dst any) error {
	args := c.ctx.QueryArgs()
	if args.Len() == 0 {
		return nil
	}
	values := make(url.Values, args.Len())
	args.VisitAll(func(k, v []byte) {
		key := string(k)
		values[key] = append(values[key], string(v))
	})
	return decodeFormValues(queryDecoder, dst, values)
}

func (c *hertzContext) bindHeaderValues(dst any) error {
	values := make(url.Values)
	c.ctx.Request.Header.VisitAll(func(k, v []byte) {
		key := string(k)
		if strings.EqualFold(key, "Host") {
			return
		}
		canon := textproto.CanonicalMIMEHeaderKey(key)
		values[canon] = append(values[canon], string(v))
	})
	if len(values) == 0 {
		return nil
	}
	return decodeFormValues(headerDecoder, dst, values)
}

// bindFormBody decodes the fields of a urlencoded or multipart form body into
// dst. hertz's own BindForm also binds URL query values, which BindForm must
// not. The media type is matched case-sensitively, as hertz does; any other
// body binds nothing.
func (c *hertzContext) bindFormBody(dst any) error {
	var values url.Values
	if len(c.ctx.Request.Header.MultipartFormBoundary()) > 0 {
		mf, err := c.ctx.MultipartForm()
		if err != nil {
			return err
		}
		values = url.Values(mf.Value)
	} else {
		args := c.ctx.PostArgs()
		values = make(url.Values, args.Len())
		args.VisitAll(func(k, v []byte) {
			key := string(k)
			values[key] = append(values[key], string(v))
		})
	}
	return decodeFormValues(formDecoder, dst, values)
}

func validateFormKeys(values url.Values) error {
	for k := range values {
		insideBracket := false
		for i := 0; i < len(k); i++ {
			switch k[i] {
			case '[':
				if insideBracket {
					return fmt.Errorf("hertzx: invalid form key %q: unexpected '['", k)
				}
				insideBracket = true
			case ']':
				if !insideBracket {
					return fmt.Errorf("hertzx: invalid form key %q: missing '['", k)
				}
				insideBracket = false
			}
		}
		if insideBracket {
			return fmt.Errorf("hertzx: invalid form key %q: missing ']'", k)
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
			err = fmt.Errorf("hertzx: form decode panic: %v", r)
		}
	}()
	return d.Decode(dst, values)
}
