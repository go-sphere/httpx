package ginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gin-gonic/gin/codec/json"
	"github.com/gin-gonic/gin/render"
	"github.com/go-playground/validator/v10"
	"github.com/go-sphere/httpx"
)

var _ httpx.Context = ginContext{}

var queryBinder = queryBinding{}

// A single-pointer value fits directly in an interface without allocating a
// wrapper. Mutable chain state belongs only to middleware invocations.
type ginContext struct {
	ctx *gin.Context
}

func newGinContext(gc *gin.Context) ginContext {
	return ginContext{ctx: gc}
}

// FromGin wraps a gin.Context as httpx.Context. Use it from a gin ErrorHandler
// to write through httpx helpers such as sphere/httpz.AbortWithJsonError.
func FromGin(gc *gin.Context) httpx.Context {
	return newGinContext(gc)
}

// Request (httpx.Request)

func (c ginContext) Method() string {
	return c.ctx.Request.Method
}

func (c ginContext) Path() string {
	return c.ctx.Request.URL.Path
}

func (c ginContext) FullPath() string {
	return c.ctx.FullPath()
}

func (c ginContext) ClientIP() string {
	return c.ctx.ClientIP()
}

func (c ginContext) Param(key string) string {
	return c.normalizeParam(key, c.ctx.Param(key))
}

func (c ginContext) Params() map[string]string {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	m := make(map[string]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		m[p.Key] = c.normalizeParam(p.Key, p.Value)
	}
	return m
}

// normalizeParam strips the leading "/" that gin includes in wildcard values,
// so /files/*filepath yields "a/b" for /files/a/b on every adapter.
func (c ginContext) normalizeParam(key, value string) string {
	if strings.HasPrefix(value, "/") && strings.Contains(c.ctx.FullPath(), "*"+key) {
		return strings.TrimPrefix(value, "/")
	}
	return value
}

func (c ginContext) Query(key string) string {
	return c.ctx.Query(key)
}

func (c ginContext) Queries() map[string][]string {
	queries := c.ctx.Request.URL.Query()
	if len(queries) == 0 {
		return nil
	}
	out := make(map[string][]string, len(queries))
	for k, v := range queries {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c ginContext) RawQuery() string {
	return c.ctx.Request.URL.RawQuery
}

func (c ginContext) Header(key string) string {
	return c.ctx.GetHeader(key)
}

func (c ginContext) Headers() map[string][]string {
	src := c.ctx.Request.Header
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		ck := textproto.CanonicalMIMEHeaderKey(k)
		out[ck] = append([]string(nil), v...)
	}
	return out
}

func (c ginContext) Cookie(name string) (string, error) {
	// Read the raw cookie value (no query-unescaping) so Cookie and Cookies
	// agree with each other and with the other adapters.
	cookie, err := c.ctx.Request.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func (c ginContext) Cookies() map[string]string {
	raw := c.ctx.Request.Cookies()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, cookie := range raw {
		out[cookie.Name] = cookie.Value
	}
	return out
}

func (c ginContext) FormValue(key string) string {
	return c.ctx.Request.FormValue(key)
}

func (c ginContext) MultipartForm() (*multipart.Form, error) {
	// Delegate to gin so the engine's configured MaxMultipartMemory is honored.
	return c.ctx.MultipartForm()
}

func (c ginContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c ginContext) BodyRaw() ([]byte, error) {
	body, err := c.ctx.GetRawData()
	if err != nil {
		return nil, err
	}
	// Restore the body so subsequent Bind*/BodyReader calls still work.
	c.ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (c ginContext) BodyReader() io.ReadCloser {
	if c.ctx.Request.Body != nil {
		return c.ctx.Request.Body
	}
	return http.NoBody
}

// Binder (httpx.Binder)

func (c ginContext) BindJSON(dst any) error {
	return bind(dst, func() error { return c.ctx.ShouldBindJSON(dst) })
}

func (c ginContext) BindQuery(dst any) error {
	return bind(dst, func() error { return queryBinder.Bind(c.ctx.Request, dst) })
}

func (c ginContext) BindForm(dst any) error {
	contentType := c.ctx.GetHeader("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/") {
		return bind(dst, func() error { return c.ctx.ShouldBindWith(dst, binding.FormMultipart) })
	}
	return bind(dst, func() error { return c.ctx.ShouldBindWith(dst, binding.Form) })
}

// BindURI binds route parameters the same way Param reads them. It cannot be
// gin's ShouldBindUri, which feeds the binder gin's raw parameter values: a
// wildcard value carries a leading "/" there, so a field tagged uri:"path"
// would bind "/a/b.txt" where Param("path") hands back "a/b.txt". Same binder
// and same tags as gin's — only the values are normalized first.
func (c ginContext) BindURI(dst any) error {
	return bind(dst, func() error { return binding.Uri.BindUri(c.uriValues(), dst) })
}

func (c ginContext) uriValues() map[string][]string {
	m := make(map[string][]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		m[p.Key] = []string{c.normalizeParam(p.Key, p.Value)}
	}
	return m
}

func (c ginContext) BindHeader(dst any) error {
	return bind(dst, func() error { return c.ctx.ShouldBindHeader(dst) })
}

// bind runs a gin binding call and reports **decode** failures as bind errors.
//
// httpx.Binder decodes and does not validate, but gin's binding package does
// both: every binding.Binding runs gin's validator on the decoded value before
// returning. That validator is the package-level binding.Validator — a
// variable shared with every other gin user in the process — so ginx cannot
// silence it without disabling validation for code it has never heard of.
// It lets gin validate and drops the verdict here instead.
//
// The cost is the validator's wasted work on every bind and a direct
// dependency on go-playground/validator purely to recognize its error types.
// The alternative, decoding without gin's bindings the way stdx does, would
// lose decode semantics the conformance suite does not pin — binding a
// multipart *multipart.FileHeader field runs through gin's unexported
// multipartRequest source, which MapFormWithTag has no equivalent for.
//
// One case this cannot cover: a process that replaces binding.Validator with
// its own StructValidator gets error values ginx has no way to tell apart from
// a decode failure, so those would surface as 400. Installing a global
// validator is asking gin to validate; httpx's contract is what it does with
// gin's own.
//
// gin's validator also panics on a typed nil inside a slice
// (reflect.Value.Interface on a zero Value). That is validation failing on
// input the decode accepted, so slice and array targets — the only shape that
// can trigger it — run under a recover that drops it. Any other panic is still
// reported, and a struct target (what generated handlers bind) keeps its
// allocation profile.
func bind(dst any, run func() error) error {
	if sliceTarget(dst) {
		return bindRecovering(run)
	}
	return httpx.WrapBindError(decodeError(run()))
}

func bindRecovering(run func() error) (err error) {
	defer func() {
		switch rec := recover(); {
		case rec == nil:
		case isValidatorPanic(rec):
			err = nil
		default:
			err = httpx.WrapBindError(fmt.Errorf("ginx: binding a slice target panicked: %v", rec))
		}
	}()
	return httpx.WrapBindError(decodeError(run()))
}

// decodeError keeps only what the decode itself reported, dropping gin's
// validation verdict. Those are the three shapes gin's default validator
// returns: validator.ValidationErrors from a struct target,
// binding.SliceValidationError from a slice one (whose elements are themselves
// validation errors, since gin builds it from per-element ValidateStruct
// calls), and *validator.InvalidValidationError for a target the validator
// refuses to inspect, such as a bare time.Time.
func decodeError(err error) error {
	if err == nil {
		return nil
	}
	var (
		fieldErrors   validator.ValidationErrors
		elementErrors binding.SliceValidationError
		invalidTarget *validator.InvalidValidationError
	)
	if errors.As(err, &fieldErrors) || errors.As(err, &elementErrors) || errors.As(err, &invalidTarget) {
		return nil
	}
	return err
}

// isValidatorPanic reports the panic gin's validator raises when it reaches a
// typed nil element: reflect.Value.Interface on the zero Value obtained from
// dereferencing it.
func isValidatorPanic(rec any) bool {
	valueErr, ok := rec.(*reflect.ValueError)
	return ok && valueErr.Kind == reflect.Invalid
}

func sliceTarget(dst any) bool {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return false
	}
	switch v.Elem().Kind() {
	case reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

// Responder (httpx.Responder)

func (c ginContext) Status(code int) {
	c.ctx.Status(code)
}

func (c ginContext) JSON(code int, v any) error {
	// Use Gin's configured codec, but marshal directly so failures return
	// as errors instead of being buried in Gin's c.Errors.
	codec := json.API
	if codec == nil {
		return errors.New("ginx: JSON codec is not configured")
	}
	b, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	c.ctx.Status(code)
	// Match Gin's JSON renderer: reuse its content-type header and let the
	// HTTP server calculate Content-Length instead of rendering as generic Data.
	(render.JSON{}).WriteContentType(c.ctx.Writer)
	if code >= 100 && code < 200 || code == http.StatusNoContent || code == http.StatusNotModified {
		c.ctx.Writer.WriteHeaderNow()
		return nil
	}
	_, err = c.ctx.Writer.Write(b)
	return err
}

func (c ginContext) Text(code int, s string) error {
	c.ctx.String(code, s)
	return nil
}

func (c ginContext) NoContent(code int) error {
	c.ctx.Status(code)
	// Commit the header: Status alone leaves Writer.Written() false, so a
	// later error would be rendered over a response the handler already
	// decided. A bodyless response is still a committed response.
	c.ctx.Writer.WriteHeaderNow()
	return nil
}

func (c ginContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	c.ctx.Data(code, contentType, b)
	return nil
}

func (c ginContext) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	if rc, ok := r.(io.Closer); ok {
		defer func() {
			_ = rc.Close()
		}()
	}
	if size < 0 {
		if contentType != "" {
			c.ctx.Header("Content-Type", contentType)
		}
		c.ctx.Status(code)
		_, err := io.Copy(c.ctx.Writer, r)
		return err
	}
	c.ctx.DataFromReader(code, size, contentType, r, nil)
	return nil
}

func (c ginContext) File(path string) error {
	c.ctx.File(path)
	return nil
}

func (c ginContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	c.ctx.Redirect(code, location)
	return nil
}

func (c ginContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c ginContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		http.SetCookie(c.ctx.Writer, cookie)
	}
}

// StateStore (httpx.StateStore)

func (c ginContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c ginContext) Get(key string) (any, bool) {
	// A stored nil is reported as absent so all adapters agree (echo/fiber
	// cannot distinguish nil from missing).
	val, ok := c.ctx.Get(key)
	if !ok || val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context accessor + Next)

func (c ginContext) Context() context.Context {
	return c.ctx.Request.Context()
}

func (c ginContext) SetContext(ctx context.Context) {
	c.ctx.Request = c.ctx.Request.WithContext(ctx)
}

func (c ginContext) Next() error {
	before := len(c.ctx.Errors)
	c.ctx.Next()

	if len(c.ctx.Errors) <= before {
		return nil
	}

	errList := make([]error, 0, len(c.ctx.Errors)-before)
	for _, err := range c.ctx.Errors[before:] {
		if err != nil {
			errList = append(errList, err.Err)
		}
	}

	return joinErrors(errList)
}

func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errors.Join(errs...)
	}
}

func (c ginContext) StatusCode() int {
	return c.ctx.Writer.Status()
}

func (c ginContext) NativeContext() any {
	return c.ctx
}

// Flush implements httpx.Flusher: the first flush commits status and headers.
func (c ginContext) Flush() error {
	c.ctx.Writer.Flush()
	return nil
}

// Stream implements httpx.Streamer: each write inside fn is flushed to the
// client immediately.
func (c ginContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	if contentType != "" {
		c.ctx.Header("Content-Type", contentType)
	}
	c.ctx.Status(code)
	c.ctx.Writer.WriteHeaderNow()
	return fn(ginFlushWriter{w: c.ctx.Writer})
}

type ginFlushWriter struct {
	w gin.ResponseWriter
}

func (fw ginFlushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.w.Flush()
	return n, err
}
