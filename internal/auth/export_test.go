package auth

// DummyHash returns the hash s verifies unknown usernames against.
func DummyHash(s *Service) string { return s.dummyHash }
