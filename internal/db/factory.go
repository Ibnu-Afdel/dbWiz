package db

// New returns an unconnected Engine for the given kind. The tui maps a detected
// container's engine to a Kind and calls this at the db boundary, keeping driver
// construction in one place. Call Connect before use.
func New(kind Kind) (Engine, error) {
	switch kind {
	case KindPostgres:
		return NewPostgres(), nil
	case KindMySQL:
		return NewMySQL(), nil
	case KindMariaDB:
		return NewMariaDB(), nil
	case KindSQLite:
		return NewSQLite(), nil
	default:
		return nil, &DBError{
			Kind:   DBErrUnsupported,
			Title:  "Unknown engine",
			Detail: "DBWiz doesn't have a driver for that engine.",
			Hint:   "press [b] to go back",
		}
	}
}
