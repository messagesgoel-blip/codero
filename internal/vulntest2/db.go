// Package vulntest2 contains fixed code demonstrating secure patterns for TEST-2 rerun.
package vulntest2

import (
	"database/sql"
	"net/http"
)

// GetUser fetches a user record using a parameterized query.
func GetUser(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	rows, err := db.Query("SELECT * FROM users WHERE id = ?", userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
}
