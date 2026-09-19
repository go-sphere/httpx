package echox

import (
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
	values, err := ctx.FormParams()
	if err != nil {
		return err
	}
	return formDecoder.Decode(dst, values)
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
	params := c.ctx.ParamValues()
	decode := c.decodesParams()
	wildcard := c.wildcardParamName()
	values := make(url.Values, len(names))
	for i, key := range names {
		value := ""
		if i < len(params) {
			value = params[i]
			if decode {
				value = decodeParamValue(value)
			}
		}
		if key == "*" && wildcard != "" {
			key = wildcard
		}
		values.Set(key, value)
	}
	return uriDecoder.Decode(dst, values)
}
