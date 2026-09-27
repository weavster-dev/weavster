package gitstore

// WorkingTreeDiff returns the paths changed in the working tree relative to
// HEAD (spec §2.12.40), as WorkingChanges lists them.
func (s *Store) WorkingTreeDiff() ([]string, error) {
	changes, err := s.WorkingChanges()
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Path
	}
	return out, err
}

// Restore restores path to its content at the given revision (spec §2.12.40).
func (s *Store) Restore(path, rev string) error {
	b, err := s.ContentAtRevision(path, rev)
	if err != nil {
		return err
	}
	return s.WriteFile(path, b)
}
