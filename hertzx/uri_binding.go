package hertzx

import (
	"net/url"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-playground/form/v4"
)

var uriDecoder = newURIDecoder()

func newURIDecoder() *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName("uri")
	return decoder
}

func bindURIWithForm(dst any, rc *app.RequestContext) error {
	if len(rc.Params) == 0 {
		return nil
	}
	values := make(url.Values, len(rc.Params))
	for _, p := range rc.Params {
		values.Set(p.Key, p.Value)
	}
	return uriDecoder.Decode(dst, values)
}
