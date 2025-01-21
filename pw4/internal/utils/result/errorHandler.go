package result

import "fmt"

// ErrorOr is a type that can hold either an error or a value.
// It can then later on be unwrapped to a zeroed-value and error, or the value and a nil error.
type ErrorOr[T any] struct {
	e error
	v T
}

// Wrap wraps a result and an error into an ErrorOr.
func Wrap[T any](result T, err error) ErrorOr[T] {
	return ErrorOr[T]{e: err, v: result}
}

// Error wraps an error into an ErrorOr.
func Error[T any](e error) ErrorOr[T] {
	return ErrorOr[T]{e: e}
}

// Of wraps a value into an ErrorOr.
func Of[T any](v T) ErrorOr[T] {
	return ErrorOr[T]{e: nil, v: v}
}

// Unwrap unwraps the ErrorOr into a value and an error.
func (e ErrorOr[T]) Unwrap() (T, error) {
	return e.v, e.e
}

// GetResult unwraps the ErrorOr into a value.
func (e ErrorOr[T]) GetResult() T {
	if e.e != nil {
		panic("ErrorOr does not contain a result")
	}

	return e.v
}

// GetResultOr unwraps the ErrorOr into a value, or a default value if the ErrorOr contains an error.
func (e ErrorOr[T]) GetResultOr(defaultValue T) T {
	if e.e != nil {
		return defaultValue
	}

	return e.v
}

// GetError unwraps the ErrorOr into an error.
func (e ErrorOr[T]) GetError() error {
	if e.e == nil {
		panic("ErrorOr does not contain an error")
	}

	return e.e
}

// IsError reports whether the ErrorOr contains an error.
func (e ErrorOr[T]) IsError() bool {
	return e.e != nil
}

// IsResult reports whether the ErrorOr contains a value.
func (e ErrorOr[T]) IsResult() bool {
	return e.e == nil
}

func (e ErrorOr[T]) String() string {
	if e.e != nil {
		return fmt.Sprintf("Error(%v)", e.e)
	}

	return fmt.Sprintf("Result(%v)", e.v)
}
