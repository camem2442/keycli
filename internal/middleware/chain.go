package middleware

// Chain composes middleware in config order: the first name is the outermost wrapper.
func Chain(mws []Middleware, inner Lookup) Lookup {
	if inner == nil {
		return nil
	}
	lookup := inner
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] == nil {
			continue
		}
		lookup = mws[i](lookup)
	}
	return lookup
}
