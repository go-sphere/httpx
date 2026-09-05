package hertzx

import (
	"reflect"

	"github.com/go-playground/validator/v10"
)

var structValidator = newStructValidator()

func newStructValidator() *validator.Validate {
	v := validator.New()
	// Match gin's convention so the same DTO validates identically on every
	// adapter (Binder contract: `binding:"required"` etc.).
	v.SetTagName("binding")
	return v
}

// validateStruct runs Binder-contract validation after a successful decode.
// Mirroring gin's semantics, only structs and pointers to structs are
// validated; other kinds pass through.
func validateStruct(obj any) error {
	if obj == nil {
		return nil
	}
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	return structValidator.Struct(obj)
}
