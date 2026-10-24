package cli

// ExitCodeError carries a subprocess exit code through RunE.
type ExitCodeError struct {
	Code int
}

func (e ExitCodeError) Error() string {
	return "exit status"
}
