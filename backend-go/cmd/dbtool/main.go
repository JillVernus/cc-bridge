package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	log.SetFlags(0)

	dbPath := flag.String("db", "../request_logs.db", "path to SQLite DB (request_logs.db)")
	apply := flag.Bool("apply", false, "apply changes (default: dry-run)")
	limit := flag.Int("limit", 10, "sample rows to print in dry-run")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := ensureColumns(db); err != nil {
		log.Fatalf("schema check: %v", err)
	}

	candidateCount, err := countCandidates(db)
	if err != nil {
		log.Fatalf("count candidates: %v", err)
	}
	log.Printf("candidates: %d", candidateCount)

	if !*apply {
		if *limit > 0 {
			if err := printSamples(db, *limit); err != nil {
				log.Fatalf("print samples: %v", err)
			}
		}
		log.Printf("dry-run complete (use --apply to update)")
		return
	}

	if candidateCount == 0 {
		log.Printf("nothing to do")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("begin: %v", err)
	}

	updated, err := backfill(tx)
	if err != nil {
		_ = tx.Rollback()
		log.Fatalf("update: %v", err)
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("commit: %v", err)
	}

	afterCount, err := countCandidates(db)
	if err != nil {
		log.Fatalf("count after: %v", err)
	}

	log.Printf("updated rows: %d", updated)
	log.Printf("candidates after: %d", afterCount)
}

func ensureColumns(db *sql.DB) error {
	cols, err := tableColumns(db, "request_logs")
	if err != nil {
		return err
	}

	required := []string{"user_id", "session_id"}
	for _, col := range required {
		if !cols[col] {
			return fmt.Errorf("missing column %q in request_logs", col)
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT name FROM pragma_table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func countCandidates(db *sql.DB) (int64, error) {
	const q = `
		SELECT COUNT(*)
		FROM request_logs
		WHERE (session_id IS NULL OR TRIM(session_id) = '')
		  AND user_id IS NOT NULL
		  AND instr(TRIM(user_id), '_account__session_') > 0
	`
	var n int64
	if err := db.QueryRow(q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func printSamples(db *sql.DB, limit int) error {
	if limit <= 0 {
		return nil
	}
	const q = `
		SELECT id, user_id, session_id,
		       substr(TRIM(user_id), 1, instr(TRIM(user_id), '_account__session_') - 1) AS parsed_user_id,
		       substr(TRIM(user_id), instr(TRIM(user_id), '_account__session_') + length('_account__session_')) AS parsed_session_id
		FROM request_logs
		WHERE (session_id IS NULL OR TRIM(session_id) = '')
		  AND user_id IS NOT NULL
		  AND instr(TRIM(user_id), '_account__session_') > 0
		LIMIT ?
	`
	rows, err := db.Query(q, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	log.Printf("sample rows:")
	for rows.Next() {
		var id string
		var userID sql.NullString
		var sessionID sql.NullString
		var parsedUserID sql.NullString
		var parsedSessionID sql.NullString
		if err := rows.Scan(&id, &userID, &sessionID, &parsedUserID, &parsedSessionID); err != nil {
			return err
		}
		u := strings.TrimSpace(userID.String)
		s := strings.TrimSpace(sessionID.String)
		pu := strings.TrimSpace(parsedUserID.String)
		ps := strings.TrimSpace(parsedSessionID.String)
		log.Printf("- id=%s user_id=%s session_id=%s -> user_id=%s session_id=%s", id, u, s, pu, ps)
	}
	return rows.Err()
}

func backfill(tx *sql.Tx) (int64, error) {
	const q = `
		WITH parsed AS (
			SELECT id,
			       substr(TRIM(user_id), 1, instr(TRIM(user_id), '_account__session_') - 1) AS new_user_id,
			       substr(TRIM(user_id), instr(TRIM(user_id), '_account__session_') + length('_account__session_')) AS new_session_id
			FROM request_logs
			WHERE (session_id IS NULL OR TRIM(session_id) = '')
			  AND user_id IS NOT NULL
			  AND instr(TRIM(user_id), '_account__session_') > 0
		)
		UPDATE request_logs
		SET user_id = (SELECT new_user_id FROM parsed WHERE parsed.id = request_logs.id),
		    session_id = (SELECT new_session_id FROM parsed WHERE parsed.id = request_logs.id)
		WHERE id IN (SELECT id FROM parsed)
	`
	res, err := tx.Exec(q)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func init() {
	if v := os.Getenv("DBTOOL_LOG_PREFIX"); v != "" {
		log.SetPrefix(v)
	}
}
