package ginx

import (
	"net/http"

	"github.com/gin-gonic/gin/binding"
)

type QueryBinding struct{}

func (QueryBinding) Name() string {
	return "query"
}

func (QueryBinding) Bind(req *http.Request, obj any) error {
	values := req.URL.Query()
	return binding.MapFormWithTag(obj, values, "query")
}
