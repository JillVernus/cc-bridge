package apikey

import (
	"log"

	"github.com/JillVernus/cc-bridge/internal/database"
)

// NewManagerWithDB creates a new API key manager using a shared database connection.
// This is used when STORAGE_BACKEND=database to consolidate all storage into one DB.
// Unlike NewManager, this version does NOT initialize schemas (handled by migrations).
func NewManagerWithDB(db database.DB) (*Manager, error) {
	m := &Manager{
		db:      db.Raw(),
		cache:   make(map[string]*ValidatedKey),
		dialect: db.Dialect(),
	}

	// Load active keys into cache
	if err := m.refreshCache(); err != nil {
		log.Printf("Warning: failed to load API keys into cache: %v", err)
	}

	log.Printf("🔑 API key manager initialized (using shared database)")
	return m, nil
}
