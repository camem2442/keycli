package file

import "filippo.io/age"

// LoadIdentityForImport exposes the age identity for bundle import.
func (s *Store) LoadIdentityForImport() (age.Identity, error) {
	return s.loadIdentity()
}
