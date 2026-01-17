package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JillVernus/cc-bridge/internal/database"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

var (
	srcType   = flag.String("src-type", "sqlite", "Source database type: sqlite or postgresql")
	srcURL    = flag.String("src-url", ".config/cc-bridge.db", "Source database connection string")
	dstType   = flag.String("dst-type", "postgresql", "Destination database type: sqlite or postgresql")
	dstURL    = flag.String("dst-url", "", "Destination database connection string")
	dryRun    = flag.Bool("dry-run", false, "Print SQL without executing")
	tablesArg = flag.String("tables", "", "Comma-separated list of tables to migrate (empty = all)")
)

// Table migration order (respects foreign key constraints)
var migrationOrder = []string{
	"settings",
	"channels",
	"model_pricing",
	"model_aliases",
	"channel_usage",
	"user_aliases",
	"api_keys",
	"request_logs",
}

func main() {
	flag.Parse()

	if *dstURL == "" && !*dryRun {
		log.Fatal("--dst-url is required (or use --dry-run)")
	}

	// Open source database
	srcDB, srcDialect, err := openDB(*srcType, *srcURL)
	if err != nil {
		log.Fatalf("Failed to open source database: %v", err)
	}
	defer srcDB.Close()

	// Determine tables to migrate
	tables := migrationOrder
	if *tablesArg != "" {
		tables = strings.Split(*tablesArg, ",")
		for i, t := range tables {
			tables[i] = strings.TrimSpace(t)
		}
	}

	if *dryRun {
		log.Println("=== DRY RUN MODE - SQL will be printed, not executed ===")
		dstDialect := database.Dialect(*dstType)
		if err := exportToSQL(srcDB, srcDialect, dstDialect, tables); err != nil {
			log.Fatalf("Export failed: %v", err)
		}
		return
	}

	// Open destination database
	dstDB, dstDialect, err := openDB(*dstType, *dstURL)
	if err != nil {
		log.Fatalf("Failed to open destination database: %v", err)
	}
	defer dstDB.Close()

	// Run migrations on destination first
	dstDBWrapper, err := wrapDB(dstDB, dstDialect)
	if err != nil {
		log.Fatalf("Failed to wrap destination database: %v", err)
	}

	log.Println("Running schema migrations on destination database...")
	if err := database.RunMigrations(dstDBWrapper); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Migrate data
	for _, table := range tables {
		if err := migrateTable(srcDB, dstDB, srcDialect, dstDialect, table); err != nil {
			log.Printf("Warning: Failed to migrate table %s: %v", table, err)
		}
	}

	log.Println("Migration completed successfully!")
}

func openDB(dbType, url string) (*sql.DB, database.Dialect, error) {
	var driver string
	var dialect database.Dialect

	switch database.Dialect(dbType) {
	case database.DialectSQLite:
		driver = "sqlite"
		dialect = database.DialectSQLite
		if !strings.Contains(url, "?") {
			url += "?_busy_timeout=5000"
		}
	case database.DialectPostgreSQL:
		driver = "postgres"
		dialect = database.DialectPostgreSQL
	default:
		return nil, "", fmt.Errorf("unsupported database type: %s", dbType)
	}

	db, err := sql.Open(driver, url)
	if err != nil {
		return nil, "", err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, "", err
	}

	return db, dialect, nil
}

func wrapDB(db *sql.DB, dialect database.Dialect) (database.DB, error) {
	switch dialect {
	case database.DialectSQLite:
		return database.NewSQLite(database.Config{URL: ""})
	case database.DialectPostgreSQL:
		cfg := database.Config{
			Type: database.DialectPostgreSQL,
			URL:  *dstURL,
		}
		return database.NewPostgreSQL(cfg)
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}
}

func exportToSQL(srcDB *sql.DB, srcDialect, dstDialect database.Dialect, tables []string) error {
	for _, table := range tables {
		exists, err := tableExistsRaw(srcDB, srcDialect, table)
		if err != nil {
			return err
		}
		if !exists {
			log.Printf("Skipping table %s (does not exist in source)", table)
			continue
		}

		log.Printf("-- Exporting table: %s", table)

		rows, err := srcDB.Query(fmt.Sprintf("SELECT * FROM %s", table))
		if err != nil {
			return fmt.Errorf("failed to query %s: %w", table, err)
		}

		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return err
		}

		count := 0
		for rows.Next() {
			values := make([]interface{}, len(cols))
			valuePtrs := make([]interface{}, len(cols))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				rows.Close()
				return err
			}

			sql := buildInsertSQL(table, cols, values, dstDialect)
			fmt.Println(sql)
			count++
		}
		rows.Close()

		log.Printf("-- Exported %d rows from %s", count, table)
	}

	return nil
}

func migrateTable(srcDB, dstDB *sql.DB, srcDialect, dstDialect database.Dialect, table string) error {
	exists, err := tableExistsRaw(srcDB, srcDialect, table)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("Skipping table %s (does not exist in source)", table)
		return nil
	}

	log.Printf("Migrating table: %s", table)

	rows, err := srcDB.Query(fmt.Sprintf("SELECT * FROM %s", table))
	if err != nil {
		return fmt.Errorf("failed to query %s: %w", table, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	count := 0
	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		if err := insertRow(dstDB, dstDialect, table, cols, values); err != nil {
			log.Printf("Warning: Failed to insert row into %s: %v", table, err)
		} else {
			count++
		}
	}

	log.Printf("Migrated %d rows to %s", count, table)
	return rows.Err()
}

func insertRow(db *sql.DB, dialect database.Dialect, table string, cols []string, values []interface{}) error {
	placeholders := make([]string, len(cols))
	for i := range cols {
		if dialect == database.DialectPostgreSQL {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		table,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := db.Exec(query, values...)
	return err
}

func buildInsertSQL(table string, cols []string, values []interface{}, dialect database.Dialect) string {
	var valueStrs []string
	for _, v := range values {
		valueStrs = append(valueStrs, formatValue(v, dialect))
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING;",
		table,
		strings.Join(cols, ", "),
		strings.Join(valueStrs, ", "),
	)
}

func formatValue(v interface{}, dialect database.Dialect) string {
	if v == nil {
		return "NULL"
	}

	switch val := v.(type) {
	case bool:
		if dialect == database.DialectPostgreSQL {
			if val {
				return "TRUE"
			}
			return "FALSE"
		}
		if val {
			return "1"
		}
		return "0"
	case int, int64, float64:
		return fmt.Sprintf("%v", val)
	case time.Time:
		if dialect == database.DialectPostgreSQL {
			return fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05.999999-07:00"))
		}
		return fmt.Sprintf("'%s'", val.Format("2006-01-02 15:04:05"))
	case []byte:
		// Check if it's JSON
		var js interface{}
		if json.Unmarshal(val, &js) == nil {
			return fmt.Sprintf("'%s'", escapeString(string(val)))
		}
		// Binary data
		if dialect == database.DialectPostgreSQL {
			return fmt.Sprintf("E'\\\\x%x'", val)
		}
		return fmt.Sprintf("X'%x'", val)
	case string:
		return fmt.Sprintf("'%s'", escapeString(val))
	default:
		return fmt.Sprintf("'%v'", escapeString(fmt.Sprintf("%v", v)))
	}
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

func tableExistsRaw(db *sql.DB, dialect database.Dialect, table string) (bool, error) {
	var query string
	var args []interface{}

	switch dialect {
	case database.DialectSQLite:
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		args = []interface{}{table}
	case database.DialectPostgreSQL:
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1"
		args = []interface{}{table}
	}

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
