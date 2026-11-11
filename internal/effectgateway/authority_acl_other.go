//go:build !darwin && !linux

package effectgateway

// Protection semantics and Unix peer authentication are unverified elsewhere.
func authorityACLProtected(int) bool { return false }
