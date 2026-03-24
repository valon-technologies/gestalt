package sqlite

func (s *Store) Warnings() []string {
	return []string{
		"using SQLite for the datastore; this is not recommended for production. See https://gestalt.run/deploy for alternatives.",
	}
}
