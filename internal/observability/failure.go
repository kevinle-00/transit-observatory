package observability

// Failure separates stable public status information from the internal cause.
type Failure struct {
	Stage         string
	Code          string
	PublicMessage string
	Err           error
}

func (f Failure) Error() string {
	if f.Err != nil {
		return f.Err.Error()
	}
	return f.PublicMessage
}

func (f Failure) Unwrap() error { return f.Err }
