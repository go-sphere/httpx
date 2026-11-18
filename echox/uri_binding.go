package echox

import (
	"errors"
	"fmt"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"

	"github.com/go-playground/form/v4"
	"github.com/labstack/echo/v4"
)

var (
	uriDecoder    = newExplicitDecoder("uri", false)
	formDecoder   = newExplicitDecoder("form", false)
	queryDecoder  = newExplicitDecoder("query", false)
	headerDecoder = newExplicitDecoder("header", true)
)

func newExplicitDecoder(tag string, canonicalHeader bool) *form.Decoder {
	decoder := form.NewDecoder()
	decoder.SetTagName(tag)
	decoder.RegisterTagNameFunc(func(f reflect.StructField) string {
		raw, ok := f.Tag.Lookup(tag)
		if !ok {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if f.Anonymous && ft.Kind() == reflect.Struct {
				return ""
			}
			return "-"
		}
		if raw == "-" || raw == "" {
			return "-"
		}
		name, opts, hasOpts := strings.Cut(raw, ",")
		if name == "-" {
			return "-"
		}
		if name == "" {
			name = f.Name
		}
		if canonicalHeader {
			name = textproto.CanonicalMIMEHeaderKey(name)
		}
		if hasOpts {
			return name + "," + opts
		}
		return name
	})
	return decoder
}

func checkBindTarget(dst any) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("echox: bind target must be a non-nil pointer")
	}
	return nil
}

func withoutRawQuery(req *http.Request) *http.Request {
	if req == nil || req.URL == nil || req.URL.RawQuery == "" {
		return req
	}
	copyReq := *req
	u := *copyReq.URL
	u.RawQuery = ""
	copyReq.URL = &u
	return &copyReq
}

func isMultipart(req *http.Request) bool {
	ct := req.Header.Get(echo.HeaderContentType)
	mediaType, _, _ := strings.Cut(strings.TrimSpace(ct), ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "multipart/form-data")
}

func bindForm(dst any, ctx echo.Context) error {
	req := ctx.Request()
	if isMultipart(req) {
		if req.MultipartForm == nil {
			iso := withoutRawQuery(req)
			err := iso.ParseMultipartForm(32 << 20)
			if iso != req {
				req.PostForm = iso.PostForm
				req.MultipartForm = iso.MultipartForm
			}
			if err != nil {
				return err
			}
		}
		values := req.PostForm
		if req.MultipartForm != nil && len(req.MultipartForm.Value) > 0 {
			values = url.Values(req.MultipartForm.Value)
		}
		return decodeFormValues(formDecoder, dst, values)
	}
	if req.PostForm == nil {
		iso := withoutRawQuery(req)
		if iso == req {
			copyReq := *req
			iso = &copyReq
		}
		iso.Method = http.MethodPost
		err := iso.ParseForm()
		req.PostForm = iso.PostForm
		if err != nil {
			return err
		}
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
