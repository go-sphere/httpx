package echox

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-playground/form/v4"
	"github.com/labstack/echo/v4"
)

var (
	uriDecoder  = newDecoder("uri")
	formDecoder = newDecoder("form")
)

func newDecoder(tag string) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
	return decoder
}

func bindForm(dst any, ctx echo.Context) error {
	// net/http rather than echo's FormParams, which recognizes a multipart body
	// only by a case-sensitive Content-Type prefix; media types are
	// case-insensitive. The memory limit is echo's. PostForm holds the body's
	// fields only, multipart ones included, never the URL query.
	req := ctx.Request()
	if err := req.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	return decodeFormValues(formDecoder, dst, req.PostForm)
}

// bindURIWithForm decodes the matched route's parameters into dst. It is a
// method on echoContext rather than a function over echo.Context because it has
// to read the parameter set exactly as Param and Params do — same decoding
// rule, same resolution of the "*" key back to the name the route was
// registered with. Binding straight off ctx.ParamNames() instead silently binds
// "" for a field tagged uri:"filepath" on a /files/*filepath route, because echo
// only knows that parameter as "*" and an unmatched tag is not an error.
func (c *echoContext) bindURIWithForm(dst any) error {
	names := c.ctx.ParamNames()
	if len(names) == 0 {
		return nil
	}
	values := make(url.Values, len(names))
	c.visitParams(func(key, value string) {
		values.Set(key, value)
	})
	return decodeFormValues(uriDecoder, dst, values)
}

func validateFormKeys(values url.Values) error {
	for k := range values {
		insideBracket := false
		for i := 0; i < len(k); i++ {
			switch k[i] {
			case '[':
				if insideBracket {
					return fmt.Errorf("echox: invalid form key %q: unexpected '['", k)
				}
				insideBracket = true
			case ']':
				if !insideBracket {
					return fmt.Errorf("echox: invalid form key %q: missing '['", k)
				}
				insideBracket = false
			}
		}
		if insideBracket {
			return fmt.Errorf("echox: invalid form key %q: missing ']'", k)
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
			err = fmt.Errorf("echox: form decode panic: %v", r)
		}
	}()
	return d.Decode(dst, values)
}
