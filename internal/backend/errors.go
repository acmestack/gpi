package backend

import "fmt"

// UnknownBackendError is returned when a cluster references a backend that is
// not registered.
type UnknownBackendError struct {
	Name string
}

func (e *UnknownBackendError) Error() string {
	return fmt.Sprintf("unknown execution backend %q", e.Name)
}
