package errx

import "errors"

// LogrKV returns controller-runtime logr-style key/value pairs for an errx.Error.
// The second return value is false when err is nil or not an *Error.
func LogrKV(err error) ([]any, bool) {
	if err == nil {
		return nil, false
	}
	var errxErr *Error
	if !errors.As(err, &errxErr) {
		return nil, false
	}
	keysAndValues := []any{
		"error.code", errxErr.Code(),
		"error.category", errxErr.Description(),
		"error.message", errxErr.Message(),
	}
	if ctx := errxErr.Context(); ctx != nil {
		for key, value := range ctx {
			keysAndValues = append(keysAndValues, "error.context."+key, value)
		}
	}
	if cause := errxErr.Cause(); cause != nil {
		keysAndValues = append(keysAndValues, "error.cause", cause.Error())
	}
	return keysAndValues, true
}
