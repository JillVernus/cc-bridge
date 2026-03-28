package requestlog

import (
	"regexp"
	"strings"
	"testing"

	"github.com/JillVernus/cc-bridge/internal/database"
)

func TestRequestLogUpdateQuery_UsesBooleanLiteralForServiceTierOverride(t *testing.T) {
	query := requestLogUpdateQuery()
	if strings.Contains(query, "THEN 1") {
		t.Fatalf("requestLogUpdateQuery should not use integer literal for boolean update:\n%s", query)
	}
	if !strings.Contains(query, "WHEN ? THEN TRUE") {
		t.Fatalf("requestLogUpdateQuery must use boolean literal TRUE:\n%s", query)
	}

	pgQuery := database.ConvertPlaceholders(query, database.DialectPostgreSQL)
	if strings.Contains(pgQuery, "THEN 1") {
		t.Fatalf("PostgreSQL query should not use integer literal for boolean update:\n%s", pgQuery)
	}
	if !regexp.MustCompile(`WHEN \$\d+ THEN TRUE`).MatchString(pgQuery) {
		t.Fatalf("PostgreSQL query lost boolean-safe service_tier_overridden clause:\n%s", pgQuery)
	}
}
