package internal

import (
	"strings"
)

const DefaultMultiErrorSeparator = ";"

// NewMultiError creates a MultiError that uses the default separator for each error.
func NewMultiError(errs []error) error {
	return &MultiError{Errors: errs, Separator: DefaultMultiErrorSeparator}
}

// MultiError is an error that aggregates multiple errors together and uses
// a separator to aggregate them when called with Error.
type MultiError struct {
	Errors    []error
	Separator string
}

func (m *MultiError) Error() string {
	errs := make([]string, len(m.Errors))
	for i, err := range m.Errors {
		errs[i] = err.Error()
	}
	return strings.Join(errs, m.Separator)
}
