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
	mode := flag.String("mode", "claude", "backfill mode: claude | codex | all")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := ensureColumns(db); err != nil {
		log.Fatalf("schema check: %v", err)
	}

	ops, err := operationsForMode(*mode)
	if err != nil {
		log.Fatalf("mode: %v", err)
	}

	for _, op := range ops {
		candidateCount, err := op.CountCandidates(db)
		if err != nil {
			log.Fatalf("count candidates (%s): %v", op.Name, err)
		}
		log.Printf("[%s] candidates: %d", op.Name, candidateCount)

		if !*apply {
			if *limit > 0 {
				if err := op.PrintSamples(db, *limit); err != nil {
					log.Fatalf("print samples (%s): %v", op.Name, err)
				}
			}
			continue
		}

		if candidateCount == 0 {
			log.Printf("[%s] nothing to do", op.Name)
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("begin (%s): %v", op.Name, err)
		}

		updated, err := op.Backfill(tx)
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("update (%s): %v", op.Name, err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("commit (%s): %v", op.Name, err)
		}

		afterCount, err := op.CountCandidates(db)
		if err != nil {
			log.Fatalf("count after (%s): %v", op.Name, err)
		}

		log.Printf("[%s] updated rows: %d", op.Name, updated)
		log.Printf("[%s] candidates after: %d", op.Name, afterCount)
	}

	if !*apply {
		log.Printf("dry-run complete (use --apply to update)")
		return
	}
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

type operation struct {
	Name            string
	CountCandidates func(db *sql.DB) (int64, error)
	PrintSamples    func(db *sql.DB, limit int) error
	Backfill        func(tx *sql.Tx) (int64, error)
}

func operationsForMode(mode string) ([]operation, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "claude":
		return []operation{operationClaude()}, nil
	case "codex":
		return []operation{operationCodex()}, nil
	case "all":
		return []operation{operationClaude(), operationCodex()}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q (expected: claude | codex | all)", mode)
	}
}

func operationClaude() operation {
	return operation{
		Name:            "claude",
		CountCandidates: countClaudeCandidates,
		PrintSamples:    printClaudeSamples,
		Backfill:        backfillClaude,
	}
}

func countClaudeCandidates(db *sql.DB) (int64, error) {
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

func printClaudeSamples(db *sql.DB, limit int) error {
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

func backfillClaude(tx *sql.Tx) (int64, error) {
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

func operationCodex() operation {
	return operation{
		Name:            "codex",
		CountCandidates: countCodexCandidates,
		PrintSamples:    printCodexSamples,
		Backfill:        backfillCodex,
	}
}

func countCodexCandidates(db *sql.DB) (int64, error) {
	const q = `
		SELECT COUNT(*)
		FROM request_logs
		WHERE endpoint = '/v1/responses'
		  AND (session_id IS NULL OR TRIM(session_id) = '')
		  AND user_id IS NOT NULL
		  AND TRIM(user_id) != ''
		  AND TRIM(user_id) != 'codex'
		  AND TRIM(user_id) NOT LIKE 'user_%'
		  AND instr(TRIM(user_id), '_account__session_') = 0
	`
	var n int64
	if err := db.QueryRow(q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func printCodexSamples(db *sql.DB, limit int) error {
	if limit <= 0 {
		return nil
	}
	const q = `
		SELECT id, endpoint, user_id, session_id
		FROM request_logs
		WHERE endpoint = '/v1/responses'
		  AND (session_id IS NULL OR TRIM(session_id) = '')
		  AND user_id IS NOT NULL
		  AND TRIM(user_id) != ''
		  AND TRIM(user_id) != 'codex'
		  AND TRIM(user_id) NOT LIKE 'user_%'
		  AND instr(TRIM(user_id), '_account__session_') = 0
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
		var endpoint sql.NullString
		var userID sql.NullString
		var sessionID sql.NullString
		if err := rows.Scan(&id, &endpoint, &userID, &sessionID); err != nil {
			return err
		}
		u := strings.TrimSpace(userID.String)
		s := strings.TrimSpace(sessionID.String)
		e := strings.TrimSpace(endpoint.String)
		log.Printf("- id=%s endpoint=%s user_id=%s session_id=%s -> user_id=codex session_id=%s", id, e, u, s, u)
	}
	return rows.Err()
}

func backfillCodex(tx *sql.Tx) (int64, error) {
	const q = `
		UPDATE request_logs
		SET session_id = TRIM(user_id),
		    user_id = 'codex'
		WHERE endpoint = '/v1/responses'
		  AND (session_id IS NULL OR TRIM(session_id) = '')
		  AND user_id IS NOT NULL
		  AND TRIM(user_id) != ''
		  AND TRIM(user_id) != 'codex'
		  AND TRIM(user_id) NOT LIKE 'user_%'
		  AND instr(TRIM(user_id), '_account__session_') = 0
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
