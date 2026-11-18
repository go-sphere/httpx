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
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gin-gonic/gin/codec/json"
	"github.com/go-playground/validator/v10"
	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Context              = ginContext{}
	_ httpx.ResponseHeaderEditor = ginContext{}
)

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
	if strings.HasPrefix(value, "/") && strings.HasSuffix(c.ctx.FullPath(), "/*"+key) {
		return strings.TrimPrefix(value, "/")
	}
	return value
}

func (c ginContext) Query(key string) string {
	if override, ok := c.ctx.Get(queryOverrideKey); ok && override == true {
		if c.ctx.Request.URL == nil {
			return ""
		}
		return c.ctx.Request.URL.Query().Get(key)
	}
	return c.ctx.Query(key)
}

func (c ginContext) Queries() map[string][]string {
	// URL.Query parses afresh on every call, so the map is already the caller's.
	queries := c.ctx.Request.URL.Query()
	if len(queries) == 0 {
		return nil
	}
	return queries
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
		out[ck] = append(out[ck], v...)
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
	if orig := c.ctx.Request.URL; orig != nil && orig.RawQuery != "" && c.ctx.Request.MultipartForm == nil {
		u := *orig
		u.RawQuery = ""
		c.ctx.Request.URL = &u
		defer func() { c.ctx.Request.URL = orig }()
	}
	// Delegate to gin so the engine's configured MaxMultipartMemory is honored.
	return c.ctx.MultipartForm()
}

func (c ginContext) FormFile(name string) (*multipart.FileHeader, error) {
	if orig := c.ctx.Request.URL; orig != nil && orig.RawQuery != "" && c.ctx.Request.MultipartForm == nil {
		u := *orig
		u.RawQuery = ""
		c.ctx.Request.URL = &u
		defer func() { c.ctx.Request.URL = orig }()
	}
	return c.ctx.FormFile(name)
}

const maxBodyPrealloc = 4 << 20

func readBody(r io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > maxBodyPrealloc {
		return io.ReadAll(r)
	}
	buf := make([]byte, 0, size+1)
	for {
		if len(buf) == cap(buf) {
			rest, err := io.ReadAll(r)
			return append(buf, rest...), err
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
}

func (c ginContext) BodyRaw() ([]byte, error) {
	if c.ctx.Request.Body == nil {
		return nil, nil
	}
	body, err := readBody(c.ctx.Request.Body, c.ctx.Request.ContentLength)
	if err != nil {
		return nil, err
	}
	_ = c.ctx.Request.Body.Close()
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
	if err := checkBindTarget(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return bind(dst, func() error { return c.ctx.ShouldBindJSON(dst) })
}

func (c ginContext) BindQuery(dst any) error {
	if err := checkBindTarget(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	if c.ctx.Request.URL == nil || c.ctx.Request.URL.RawQuery == "" {
		return nil
	}
	plan := structBindPlan(dst, "query")
	if plan != nil && !plan.hasExplicit {
		return nil
	}
	if plan == nil || !plan.hasUntagged {
		return bind(dst, func() error { return queryBinder.Bind(c.ctx.Request, dst) })
	}
	saved := plan.snapshot(dst)
	filtered := plan.filterValues(c.ctx.Request.URL.Query())
	err := bind(dst, func() error { return binding.MapFormWithTag(dst, filtered, "query") })
	if err == nil {
		restoreUntagged(dst, saved)
	}
	return err
}

func isMultipart(req *http.Request) bool {
	ct := req.Header.Get("Content-Type")
	mediaType, _, _ := strings.Cut(strings.TrimSpace(ct), ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "multipart/form-data")
}

// BindForm binds the form body only. Both bindings it picks read body values
// (gin's multipart binding reads MultipartForm, FormPost reads PostForm);
// gin's binding.Form would read req.Form, which merges in the URL query.
func (c ginContext) BindForm(dst any) error {
	if err := checkBindTarget(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	plan := structBindPlan(dst, "form")
	if plan != nil && !plan.hasExplicit {
		return nil
	}
	if orig := c.ctx.Request.URL; orig != nil && orig.RawQuery != "" {
		u := *orig
		u.RawQuery = ""
		c.ctx.Request.URL = &u
		defer func() { c.ctx.Request.URL = orig }()
	}
	if plan == nil || !plan.hasUntagged {
		if isMultipart(c.ctx.Request) {
			return bind(dst, func() error { return c.ctx.ShouldBindWith(dst, binding.FormMultipart) })
		}
		return bind(dst, func() error { return c.ctx.ShouldBindWith(dst, binding.FormPost) })
	}
	saved := plan.snapshot(dst)
	var err error
	if isMultipart(c.ctx.Request) {
		mf, parseErr := c.ctx.MultipartForm()
		if parseErr != nil {
			return httpx.WrapBindError(parseErr)
		}
		reqCopy := *c.ctx.Request
		reqCopy.MultipartForm = &multipart.Form{
			Value: plan.filterValues(mf.Value),
			File:  plan.filterFiles(mf.File),
		}
		err = bind(dst, func() error { return binding.FormMultipart.Bind(&reqCopy, dst) })
	} else {
		if parseErr := c.ctx.Request.ParseForm(); parseErr != nil {
			return httpx.WrapBindError(parseErr)
		}
		reqCopy := *c.ctx.Request
		reqCopy.PostForm = plan.filterValues(c.ctx.Request.PostForm)
		err = bind(dst, func() error { return binding.FormPost.Bind(&reqCopy, dst) })
	}
	if err == nil {
		restoreUntagged(dst, saved)
	}
	return err
}

// BindURI binds route parameters the same way Param reads them. It cannot be
// gin's ShouldBindUri, which feeds the binder gin's raw parameter values: a
// wildcard value carries a leading "/" there, so a field tagged uri:"path"
// would bind "/a/b.txt" where Param("path") hands back "a/b.txt".
func (c ginContext) BindURI(dst any) error {
	if err := checkBindTarget(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	if len(c.ctx.Params) == 0 {
		return nil
	}
	plan := structBindPlan(dst, "uri")
	if plan != nil && !plan.hasExplicit {
		return nil
	}
	var saved []savedField
	if plan != nil && plan.hasUntagged {
		saved = plan.snapshot(dst)
	}
	var buf [8][1]string
	m := make(map[string][]string, len(c.ctx.Params))
	for i, p := range c.ctx.Params {
		if plan != nil && plan.hasUntagged && !plan.allows(p.Key) {
			continue
		}
		val := c.normalizeParam(p.Key, p.Value)
		if i < len(buf) {
			buf[i][0] = val
			m[p.Key] = buf[i][:]
		} else {
			m[p.Key] = []string{val}
		}
	}
	err := httpx.WrapBindError(binding.MapFormWithTag(dst, m, "uri"))
	if err == nil && len(saved) > 0 {
		restoreUntagged(dst, saved)
	}
	return err
}

func (c ginContext) BindHeader(dst any) error {
	if err := checkBindTarget(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	plan := structBindPlan(dst, "header")
	if plan != nil && !plan.hasExplicit {
		return nil
	}
	if plan == nil || !plan.hasUntagged {
		return bind(dst, func() error { return c.ctx.ShouldBindHeader(dst) })
	}
	saved := plan.snapshot(dst)
	hdr := make(http.Header, len(plan.allowedKeys))
	for k, vals := range c.ctx.Request.Header {
		if plan.allows(k) {
			hdr[k] = vals
		}
	}
	reqCopy := &http.Request{Header: hdr}
	err := bind(dst, func() error { return binding.Header.Bind(reqCopy, dst) })
	if err == nil {
		restoreUntagged(dst, saved)
	}
	return err
}

// bind runs a gin binding call and reports **decode** failures as bind errors.
//
// httpx.Binder decodes and does not validate, but gin's binding package does
// both, through the process-wide binding.Validator variable. Silencing that
// would disable validation for every other gin user in the same binary, so ginx
// lets gin validate and drops the verdict here, at the cost of the validator's
// wasted work and a go-playground/validator dependency purely to recognize its
// error types. Reimplementing the decoding the way stdx does is not an option:
// a multipart *multipart.FileHeader field goes through gin's unexported
// multipartRequest source, which MapFormWithTag has no equivalent for.
//
// A process that replaces binding.Validator with its own StructValidator
// returns error values ginx cannot tell apart from a decode failure, so those
// surface as 400.
//
// gin's validator also panics on a typed nil inside a slice
// (reflect.Value.Interface on a zero Value). That is validation failing on
// input the decode accepted, so slice and array targets — the only shape that
// can trigger it — run under a recover that drops it, but only once the
// panicking stack confirms gin's validator raised it (see
// panickedInGinValidator). Any other panic, from either phase, is reported as a
// bind failure, and a struct target keeps its allocation profile.
func bind(dst any, run func() error) error {
	if needsValidatorRecover(dst) {
		return bindRecovering(run)
	}
	return httpx.WrapBindError(decodeError(run()))
}

func bindRecovering(run func() error) (err error) {
	defer func() {
		switch rec := recover(); {
		case rec == nil:
		case isValidatorPanic(rec) && panickedInGinValidator():
			err = nil
		default:
			err = httpx.WrapBindError(fmt.Errorf("ginx: binding a slice target panicked: %v", rec))
		}
	}()
	return httpx.WrapBindError(decodeError(run()))
}

// ginValidatorFrame is the function that raises the one panic ginx drops: gin's
// default validator dereferencing a typed nil slice element.
const ginValidatorFrame = "github.com/gin-gonic/gin/binding.(*defaultValidator).ValidateStruct"

// panickedInGinValidator reports whether the panic currently being recovered
// was raised underneath gin's default validator.
//
// It is what keeps a dropped verdict from becoming a dropped *decode*. recover
// alone cannot tell them apart: bindRecovering wraps a single
// binding.Binding.Bind call, which decodes and validates with no seam between
// them, so a panic from either phase arrives at the same defer wearing the same
// *reflect.ValueError. Dropping the validator's is correct — the destination is
// already fully populated — while dropping a decode panic returns nil over a
// half-written destination where the handler should have answered 400.
//
// The panicking frames are still on the stack while a deferred function runs,
// and gin validates only after the decode returned no error, so ValidateStruct
// sitting below the panic proves the decode completed. The panic value itself
// carries no origin, and a decode that panicked part way through a slice leaves
// the destination looking like a successful one.
//
// Matching a gin-internal symbol name keeps this failing closed: a release that
// renames the function turns a typed nil element into a 400 instead of a silent
// drop. Matching the panic shape alone would fail open.
func panickedInGinValidator() bool {
	// Skip this frame; the validator sits just below the panic, so the window
	// only has to cover its recursion down to the slice element.
	var pcs [32]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function == ginValidatorFrame {
			return true
		}
		if !more {
			return false
		}
	}
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

func needsValidatorRecover(dst any) bool {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return false
	}
	switch v.Elem().Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

// Responder (httpx.Responder)

func (c ginContext) Status(code int) {
	c.ctx.Status(code)
}

// Content-Type values are assigned as ready-made slices, as gin's renderers
// do, which saves an allocation per response. append on a full slice always
// reallocates, so a later Header().Add cannot write into the shared value.
var (
	jsonContentType = []string{"application/json; charset=utf-8"}
	textContentType = []string{"text/plain; charset=utf-8"}
)

// setContentType labels the response with v unless it is already committed,
// so ResponseHeaderValues never reports a type the client did not receive.
// With keep, a Content-Type the handler set itself wins.
func (c ginContext) setContentType(v []string, keep bool) {
	if c.ctx.Writer.Written() {
		return
	}
	h := c.ctx.Writer.Header()
	if keep {
		if _, ok := h["Content-Type"]; ok {
			return
		}
	}
	h["Content-Type"] = v
}

func isInformational(code int) bool {
	return code >= 100 && code < 200 && code != http.StatusSwitchingProtocols
}

func bodyless(code int) bool {
	return code >= 100 && code < 200 || code == http.StatusNoContent || code == http.StatusNotModified
}

func (c ginContext) commitsBodyless(code int) bool {
	if c.ctx.Writer.Written() {
		return bodyless(c.ctx.Writer.Status())
	}
	return bodyless(code)
}

func (c ginContext) commitStatus(code int) {
	if c.ctx.Writer.Written() {
		return
	}
	if isInformational(code) {
		switch w := c.ctx.Writer.(type) {
		case *stdWriterBridge:
			w.WriteHeader(code)
		case interface{ Unwrap() http.ResponseWriter }:
			if uw := w.Unwrap(); uw != nil {
				uw.WriteHeader(code)
			}
		}
		return
	}
	c.ctx.Status(code)
	c.ctx.Writer.WriteHeaderNow()
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
	bl := c.commitsBodyless(code)
	if !isInformational(code) {
		c.ctx.Status(code)
	}
	c.setContentType(jsonContentType, true)
	if bl {
		c.commitStatus(code)
		return nil
	}
	_, err = c.ctx.Writer.Write(b)
	return err
}

// Text, like Bytes, writes directly rather than through gin's Render, which
// records a write error in c.Errors and returns nothing.
func (c ginContext) Text(code int, s string) error {
	bl := c.commitsBodyless(code)
	if !isInformational(code) {
		c.ctx.Status(code)
	}
	c.setContentType(textContentType, false)
	if bl {
		c.commitStatus(code)
		return nil
	}
	// An empty s still commits: gin's WriteString writes the header first.
	_, err := c.ctx.Writer.WriteString(s)
	return err
}

func (c ginContext) NoContent(code int) error {
	c.commitStatus(code)
	return nil
}

func (c ginContext) Bytes(code int, b []byte, contentType string) error {
	bl := c.commitsBodyless(code)
	if !c.ctx.Writer.Written() {
		if contentType == "" {
			contentType = http.DetectContentType(b)
		}
		c.setContentType([]string{contentType}, false)
	}
	if !isInformational(code) {
		c.ctx.Status(code)
	}
	if bl {
		c.commitStatus(code)
		return nil
	}
	_, err := c.ctx.Writer.Write(b)
	return err
}

func (c ginContext) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	if rc, ok := r.(io.Closer); ok {
		defer func() {
			_ = rc.Close()
		}()
	}
	bl := c.commitsBodyless(code)
	if contentType != "" {
		c.setContentType([]string{contentType}, false)
	}
	if !bl && r != nil && size >= 0 && !c.ctx.Writer.Written() {
		c.ctx.Writer.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	c.commitStatus(code)
	if bl || r == nil {
		return nil
	}
	_, err := io.Copy(c.ctx.Writer, r)
	return err
}

func (c ginContext) File(path string) error {
	return httpx.ServeFile(c.ctx.Writer, c.ctx.Request, path)
}

func (c ginContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	if c.ctx.Writer.Written() {
		return nil
	}
	c.ctx.Redirect(code, location)
	return nil
}

func (c ginContext) SetHeader(key, value string) {
	if c.ctx.Writer.Written() {
		return
	}
	// Not gin's c.Header, which deletes the header for an empty value.
	c.ctx.Writer.Header().Set(key, value)
}

func (c ginContext) AddHeader(key, value string) {
	if c.ctx.Writer.Written() {
		return
	}
	c.ctx.Writer.Header().Add(key, value)
}

func (c ginContext) ResponseHeaderValues(key string) []string {
	return slices.Clone(c.ctx.Writer.Header().Values(key))
}

func (c ginContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil && !c.ctx.Writer.Written() {
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

// Context (context.Context accessor)

func (c ginContext) Context() context.Context {
	return c.ctx.Request.Context()
}

func (c ginContext) SetContext(ctx context.Context) {
	if ctx != nil {
		c.ctx.Request = c.ctx.Request.WithContext(ctx)
	}
}

func (c ginContext) StatusCode() int {
	return c.ctx.Writer.Status()
}

// Committed reports whether the response header has been written. gin's
// ResponseWriter tracks it for its own render path, so this is the same bit
// gin consults, not a second one kept alongside.
func (c ginContext) Committed() bool {
	return c.ctx.Writer.Written()
}

func (c ginContext) NativeContext() any {
	return c.ctx
}

// Flush implements httpx.Flusher: the first flush commits status and headers.
func (c ginContext) Flush() error {
	if !c.ctx.Writer.Written() {
		c.ctx.Writer.WriteHeaderNow()
	}
	return flushHTTPWriter(c.ctx.Writer)
}

// Stream implements httpx.Streamer: the status line and headers are flushed
// to the client before fn runs, and so is each write inside fn.
func (c ginContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	bl := c.commitsBodyless(code)
	if contentType != "" {
		c.setContentType([]string{contentType}, false)
	}
	c.commitStatus(code)
	// WriteHeader in net/http only buffers the header; the flush sends it.
	if err := c.Flush(); err != nil {
		return err
	}
	if fn == nil {
		return nil
	}
	if bl {
		return fn(ginDiscardFlushWriter{c: c})
	}
	return fn(ginFlushWriter{c: c})
}

type ginFlushWriter struct {
	c ginContext
}

func (fw ginFlushWriter) Write(p []byte) (int, error) {
	n, err := fw.c.ctx.Writer.Write(p)
	if err != nil {
		return n, err
	}
	return n, fw.c.Flush()
}

type ginDiscardFlushWriter struct {
	c ginContext
}

func (fw ginDiscardFlushWriter) Write(p []byte) (int, error) {
	if err := fw.c.Flush(); err != nil {
		return 0, err
	}
	return len(p), nil
}
