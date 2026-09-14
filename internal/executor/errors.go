package executor

import "fmt"

// PermanentError wraps an execution error to signal that retrying it would
// not help (e.g. the job's payload is malformed, or a remote endpoint
// rejected the request with a 4xx). The worker checks for this via
// errors.As and skips the retry loop, going straight to FAILED instead of
// burning through MaxAttempts on a request that can never succeed.
type PermanentError struct {
	Err error
}

// NewPermanentError wraps err so the worker treats the failure as terminal.
func NewPermanentError(err error) error {
	return &PermanentError{Err: err}
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent: %v", e.Err)
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}
