package effectgateway

// Linux POSIX access ACLs cannot grant group-class permissions outside the
// mode's group bits, which already gate config reads and directory writes.
func authorityACLProtected(int) bool { return true }
