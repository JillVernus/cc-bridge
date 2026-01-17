package database

import "fmt"

// DialectHelper provides dialect-specific SQL helpers
type DialectHelper struct {
	dialect Dialect
}

// NewDialectHelper creates a new dialect helper
func NewDialectHelper(dialect Dialect) *DialectHelper {
	return &DialectHelper{dialect: dialect}
}

// Placeholder returns the placeholder for the nth parameter (1-indexed)
func (h *DialectHelper) Placeholder(n int) string {
	switch h.dialect {
	case DialectPostgreSQL:
		return fmt.Sprintf("$%d", n)
	default:
		return "?"
	}
}

// AutoIncrement returns the auto-increment column definition
func (h *DialectHelper) AutoIncrement() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "SERIAL"
	default:
		return "INTEGER PRIMARY KEY AUTOINCREMENT"
	}
}

// AutoIncrementPK returns the primary key definition for auto-increment columns
func (h *DialectHelper) AutoIncrementPK() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "SERIAL PRIMARY KEY"
	default:
		return "INTEGER PRIMARY KEY AUTOINCREMENT"
	}
}

// BlobType returns the BLOB type for the dialect
func (h *DialectHelper) BlobType() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "BYTEA"
	default:
		return "BLOB"
	}
}

// DatetimeType returns the datetime type for the dialect
func (h *DialectHelper) DatetimeType() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "TIMESTAMP WITH TIME ZONE"
	default:
		return "DATETIME"
	}
}

// JSONType returns the JSON type for the dialect
func (h *DialectHelper) JSONType() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "JSONB"
	default:
		return "TEXT"
	}
}

// BooleanType returns the boolean type for the dialect
func (h *DialectHelper) BooleanType() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "BOOLEAN"
	default:
		return "INTEGER" // SQLite stores boolean as 0/1
	}
}

// CurrentTimestamp returns the current timestamp function for the dialect
func (h *DialectHelper) CurrentTimestamp() string {
	switch h.dialect {
	case DialectPostgreSQL:
		return "NOW()"
	default:
		return "CURRENT_TIMESTAMP"
	}
}

// ColumnExistsQuery returns a query to check if a column exists
func (h *DialectHelper) ColumnExistsQuery(table, column string) string {
	switch h.dialect {
	case DialectPostgreSQL:
		return fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = '%s' AND column_name = '%s'
		`, table, column)
	default:
		return fmt.Sprintf(
			`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'`,
			table, column,
		)
	}
}

// TableExistsQuery returns a query to check if a table exists
func (h *DialectHelper) TableExistsQuery(table string) string {
	switch h.dialect {
	case DialectPostgreSQL:
		return fmt.Sprintf(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_name = '%s'
		`, table)
	default:
		return fmt.Sprintf(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='%s'`,
			table,
		)
	}
}

// OnConflictUpdate returns the ON CONFLICT clause for upsert
// For PostgreSQL, the conflict target is the constraint name or column list
// For SQLite, it's the same syntax
func (h *DialectHelper) OnConflictUpdate(conflictTarget string, updateCols []string) string {
	updates := ""
	for i, col := range updateCols {
		if i > 0 {
			updates += ", "
		}
		updates += fmt.Sprintf("%s = EXCLUDED.%s", col, col)
	}
	return fmt.Sprintf("ON CONFLICT(%s) DO UPDATE SET %s", conflictTarget, updates)
}

// LimitOffset returns the LIMIT/OFFSET clause for pagination
func (h *DialectHelper) LimitOffset(limit, offset int) string {
	if limit <= 0 {
		return ""
	}
	if offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	}
	return fmt.Sprintf("LIMIT %d", limit)
}

// ConvertQuery converts a query with ? placeholders to dialect-specific format
func (h *DialectHelper) ConvertQuery(query string) string {
	return ConvertPlaceholders(query, h.dialect)
}
