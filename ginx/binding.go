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
	if err := binding.MapFormWithTag(obj, values, "query"); err != nil {
		return err
	}
	// Run struct validation so `binding:"required"` works for query fields,
	// matching gin's built-in bindings.
	if binding.Validator == nil {
		return nil
	}
	return binding.Validator.ValidateStruct(obj)
}
