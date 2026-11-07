package managerconf

import (
	"encoding/json"
	"os"
	"time"
)

// The built-in manager's own database (docs/server/server.md "The default manager's own database"): where it is, in its
// manager.json, with the keys a manager made in the app uses for its own (database_url, database_container), and when
// its data moved there out of the database it shared with the hub (DatabaseMovedKey).

// DatabaseMovedKey is manager.json's record that the built-in manager's data moved into its own database: until it is
// there, the shared database stays the one in use (a move that did not finish is done again at the next start).
const DatabaseMovedKey = "moved_to_own_database"

// OwnDatabase is the built-in manager's own database as its manager.json has it.
type OwnDatabase struct {
	// URL is where it is: a container's as made (its own password), or the hub's server's with Name (derived at each
	// start from the hub's database_url: no credentials of the hub's kept here).
	URL string `json:"database_url,omitempty"`
	// Name is the second database's name in the hub's Postgres server (database_name); "" for a container.
	Name string `json:"database_name,omitempty"`
	// Container is the Docker container it runs in when the manager made one (persona-mgr-assistant-pg); "" for a
	// database in another Postgres (the hub's server, a second database).
	Container string `json:"database_container,omitempty"`
	// Location is where one is made when there is none yet (database_location: hub_server, container); "": the
	// default.
	Location string `json:"database_location,omitempty"`
	// Moved is when its data moved there; zero while it has not (yet).
	Moved time.Time `json:"-"`
}

// ReadOwnDatabase reads it from the built-in manager's manager.json in the data directory home (nothing when there is
// none, or it does not read).
func ReadOwnDatabase(home string) OwnDatabase {
	var d OwnDatabase
	b, err := os.ReadFile(DefaultFile(home))
	if err != nil {
		return d
	}
	o, err := ParseObject(b)
	if err != nil {
		return d
	}
	for key, out := range map[string]*string{"database_url": &d.URL, "database_name": &d.Name, "database_container": &d.Container,
		"database_location": &d.Location} {
		if raw, ok := o.Get(key); ok {
			_ = json.Unmarshal(raw, out)
		}
	}
	if raw, ok := o.Get(DatabaseMovedKey); ok {
		var m struct {
			At time.Time `json:"at"`
		}
		if json.Unmarshal(raw, &m) == nil {
			d.Moved = m.At
		}
	}
	return d
}

// WriteOwnDatabase writes d into the built-in manager's manager.json (its other keys as they are); a non-zero Moved
// records the move.
func WriteOwnDatabase(home string, d OwnDatabase) error {
	return Edit(DefaultFile(home), func(o *Object) error {
		for key, v := range map[string]string{"database_container": d.Container, "database_name": d.Name} {
			if v == "" {
				o.Del(key)
			} else if err := o.Set(key, v); err != nil {
				return err
			}
		}
		if d.Container != "" { // a container's address, with its own password; the hub's server's is derived
			if err := o.Set("database_url", d.URL); err != nil {
				return err
			}
		} else {
			o.Del("database_url")
		}
		if !d.Moved.IsZero() {
			return o.Set(DatabaseMovedKey, map[string]any{"at": d.Moved.UTC()})
		}
		return nil
	})
}
