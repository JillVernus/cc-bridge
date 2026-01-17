#!/bin/bash
# migrate-to-postgres.sh
# Migrates cc-bridge data from SQLite to PostgreSQL

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}CC-Bridge: SQLite → PostgreSQL Migration${NC}"
echo "=========================================="

# Default values
SQLITE_PATH=".config/cc-bridge.db"
PG_HOST="${DB_HOST:-localhost}"
PG_PORT="${DB_PORT:-5432}"
PG_DB="${DB_NAME:-ccbridge}"
PG_USER="${DB_USER:-ccbridge}"
PG_PASSWORD="${DB_PASSWORD:-changeme}"
PG_SSLMODE="${DB_SSLMODE:-disable}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --sqlite)
            SQLITE_PATH="$2"
            shift 2
            ;;
        --pg-host)
            PG_HOST="$2"
            shift 2
            ;;
        --pg-port)
            PG_PORT="$2"
            shift 2
            ;;
        --pg-db)
            PG_DB="$2"
            shift 2
            ;;
        --pg-user)
            PG_USER="$2"
            shift 2
            ;;
        --pg-password)
            PG_PASSWORD="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --sqlite PATH       SQLite database path (default: .config/cc-bridge.db)"
            echo "  --pg-host HOST      PostgreSQL host (default: localhost)"
            echo "  --pg-port PORT      PostgreSQL port (default: 5432)"
            echo "  --pg-db DB          PostgreSQL database name (default: ccbridge)"
            echo "  --pg-user USER      PostgreSQL user (default: ccbridge)"
            echo "  --pg-password PASS  PostgreSQL password (default: changeme)"
            echo "  --dry-run           Print SQL without executing"
            echo "  --help              Show this help"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Build PostgreSQL connection string
PG_URL="postgres://${PG_USER}:${PG_PASSWORD}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=${PG_SSLMODE}"

# Check if SQLite database exists
if [ ! -f "$SQLITE_PATH" ]; then
    echo -e "${RED}Error: SQLite database not found at $SQLITE_PATH${NC}"
    exit 1
fi

echo -e "${YELLOW}Source:${NC} $SQLITE_PATH"
echo -e "${YELLOW}Target:${NC} postgres://${PG_USER}:****@${PG_HOST}:${PG_PORT}/${PG_DB}"
echo ""

# Check if running in Docker or has the binary
if command -v /app/cc-bridge &> /dev/null; then
    # Running inside cc-bridge container
    MIGRATE_CMD="/app/dbmigrate"
elif [ -f "./dist/dbmigrate" ]; then
    MIGRATE_CMD="./dist/dbmigrate"
elif [ -f "./backend-go/cmd/dbmigrate/main.go" ]; then
    echo -e "${YELLOW}Building migration tool...${NC}"
    cd backend-go
    go build -o ../dist/dbmigrate ./cmd/dbmigrate
    cd ..
    MIGRATE_CMD="./dist/dbmigrate"
else
    echo -e "${RED}Error: Migration tool not found. Build it first:${NC}"
    echo "  cd backend-go && go build -o ../dist/dbmigrate ./cmd/dbmigrate"
    exit 1
fi

# Run migration
if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}Running in dry-run mode...${NC}"
    $MIGRATE_CMD \
        --src-type sqlite \
        --src-url "$SQLITE_PATH" \
        --dst-type postgresql \
        --dry-run
else
    echo -e "${YELLOW}Starting migration...${NC}"
    $MIGRATE_CMD \
        --src-type sqlite \
        --src-url "$SQLITE_PATH" \
        --dst-type postgresql \
        --dst-url "$PG_URL"

    echo ""
    echo -e "${GREEN}Migration completed!${NC}"
    echo ""
    echo "Next steps:"
    echo "1. Update your docker-compose.yml or environment variables:"
    echo "   STORAGE_BACKEND=database"
    echo "   DATABASE_TYPE=postgresql"
    echo "   DATABASE_URL=$PG_URL"
    echo ""
    echo "2. Restart cc-bridge:"
    echo "   docker compose -f docker-compose.postgres.yml up -d"
fi
