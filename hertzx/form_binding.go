package hertzx

import (
	"fmt"
	"net/url"

	"github.com/go-playground/form/v4"
)

var formDecoder = newFormDecoder()

func newFormDecoder() *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName("form")
	return decoder
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
