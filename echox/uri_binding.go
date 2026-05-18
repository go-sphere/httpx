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
