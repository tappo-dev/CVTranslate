package platform

import "errors"

// nativeDiagnostic is created only at first-party native adapter boundaries
// whose errors contain fixed explanations or OS codes, never inspected text.
type nativeDiagnostic struct {
	cause   error
	message string
}

func (failure *nativeDiagnostic) Error() string {
	if failure.message != "" {
		return failure.message
	}
	return failure.cause.Error()
}
func (failure *nativeDiagnostic) Unwrap() error { return failure.cause }

func describeNativeError(err error) error {
	if err == nil {
		return nil
	}
	var diagnostic *nativeDiagnostic
	if errors.As(err, &diagnostic) {
		return err
	}
	return &nativeDiagnostic{cause: err}
}

// operation is a fixed first-party phase label, never inspected text. Only the
// vetted diagnostic may be displayed; outer error messages can contain payloads.
func describeNativeOperation(operation string, err error) error {
	detail := DiagnosticMessage(err)
	if detail == "" {
		return err
	}
	return &nativeDiagnostic{cause: err, message: operation + ": " + detail}
}

// DiagnosticMessage excludes arbitrary provider/storage errors. Callers can
// add these vetted native details to a generic user-facing failure explanation.
func DiagnosticMessage(err error) string {
	var diagnostic *nativeDiagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Error()
	}
	if errors.Is(err, ErrUnsupported) {
		return ErrUnsupported.Error()
	}
	return ""
}
