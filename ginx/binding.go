package ginx

import (
	"net/http"

	"github.com/gin-gonic/gin/binding"
)

// queryBinding is gin's missing `query`-tag binding: gin.binding reads the
// `form` tag and parses the request body along with the query string, so the
// adapter supplies a query-only binder. It is unexported because it implements
// nothing a caller outside this package can use — gin's own binding.Binding
// interface is satisfied incidentally, not as a promise.
//
// MapFormWithTag is gin's own field mapping, so the decode rules (defaults,
// time formats, collection formats, unexported fields) are gin's. It is the
// one binder in this package that does not reach gin's validator, because it
// never calls gin's validate(); see bind in context.go for why the others'
// verdict is dropped.
type queryBinding struct{}

func (queryBinding) Name() string {
	return "query"
}

func (queryBinding) Bind(req *http.Request, obj any) error {
	return binding.MapFormWithTag(obj, req.URL.Query(), "query")
}
