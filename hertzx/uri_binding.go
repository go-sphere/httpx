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

// bindURIWithForm decodes the matched route's parameters into dst. The values
// go through normalizeParam, the same step Param applies, so the binder cannot
// report a wildcard differently from Param — hertz's router happens to hand
// back catch-all values without the leading "/" that gin's does, but that is
// hertz's choice, not this adapter's contract.
func (c *hertzContext) bindURIWithForm(dst any) error {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	values := make(url.Values, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		values.Set(p.Key, c.normalizeParam(p.Key, p.Value))
	}
	return uriDecoder.Decode(dst, values)
}
