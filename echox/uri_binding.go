package echox

import (
	"net/url"

	"github.com/go-playground/form/v4"
	"github.com/labstack/echo/v4"
)

var uriDecoder = newURIDecoder()

func newURIDecoder() *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName("uri")
	return decoder
}

func bindURIWithForm(dst any, ctx echo.Context) error {
	names := ctx.ParamNames()
	if len(names) == 0 {
		return nil
	}
	params := ctx.ParamValues()
	values := make(url.Values, len(names))
	for i, key := range names {
		value := ""
		if i < len(params) {
			value = params[i]
		}
		values.Set(key, value)
	}
	return uriDecoder.Decode(dst, values)
}
