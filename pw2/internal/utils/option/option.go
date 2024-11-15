package option

import "fmt"

// Option represents a value that may or may not be present.
type Option[T any] struct {
	value   T
	isEmpty bool
}

// Some creates a new Option with a value.
func Some[T any](value T) Option[T] {
	return Option[T]{value: value, isEmpty: false}
}

// None creates a new empty Option.
func None[T any]() Option[T] {
	return Option[T]{isEmpty: true}
}

// Reports whether the Option is empty.
func (o Option[T]) IsNone() bool {
	return o.isEmpty
}

// Returns the value of the Option. Panics if the Option is empty.
func (o Option[T]) Get() T {
	if o.isEmpty {
		panic("Option is empty")
	}
	return o.value
}

// Returns the value of the Option, or a default value if the Option is empty.
func (o Option[T]) GetOrElse(defaultValue T) T {
	if o.isEmpty {
		return defaultValue
	}
	return o.value
}

func (o Option[T]) String() string {
	if o.isEmpty {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", o.value)
}
