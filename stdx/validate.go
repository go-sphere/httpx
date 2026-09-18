package stdx

import (
	"errors"
	"fmt"
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
// It mirrors gin's validator semantics: structs and pointers to structs are
// validated, and every element of a slice or array is validated individually
// (matching gin's SliceValidationError accumulation). Other kinds pass through.
func validateStruct(obj any) error {
	if obj == nil {
		return nil
	}
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		if v.Elem().Kind() != reflect.Struct {
			// Unwrap pointer chains such as **T: validator.Struct only
			// accepts one level, so passing them through would reject a
			// valid target with a 400.
			return validateStruct(v.Elem().Interface())
		}
		return structValidator.Struct(obj)
	case reflect.Struct:
		return structValidator.Struct(obj)
	case reflect.Slice, reflect.Array:
		var errs []error
		for i := range v.Len() {
			elem := v.Index(i)
			if elem.Kind() == reflect.Pointer && elem.IsNil() {
				// A null element cannot satisfy the element type's rules and
				// would nil-dereference in the handler, so it is reported like
				// any other validation failure. gin's validator panics on this
				// input, so rejecting it is also what keeps the adapters in
				// parity. A nil *target* is different: there is nothing to
				// validate, which the Pointer case above allows.
				errs = append(errs, fmt.Errorf("element %d is null", i))
				continue
			}
			if err := validateStruct(elem.Interface()); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	default:
		return nil
	}
}
