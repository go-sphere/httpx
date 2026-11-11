package hertzx

import (
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
	return formDecoder.Decode(dst, values)
}
