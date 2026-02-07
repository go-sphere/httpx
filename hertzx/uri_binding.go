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

func bindURIWithForm(dst any, params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	values := make(url.Values, len(params))
	for key, value := range params {
		values.Set(key, value)
	}
	return uriDecoder.Decode(dst, values)
}
