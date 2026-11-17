package hertzx

import (
	"net/url"

	"github.com/go-playground/form/v4"
)

var uriDecoder = newURIDecoder()

func newURIDecoder() *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName("uri")
	return decoder
}

// bindURIWithForm decodes the matched route's parameters into dst, reading
// the same values Param returns. hertz hands back a wildcard without a leading
// "/", which is what the contract asks for; httpxtest pins it.
func (c *hertzContext) bindURIWithForm(dst any) error {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	values := make(url.Values, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		values.Set(p.Key, p.Value)
	}
	return decodeFormValues(uriDecoder, dst, values)
}
