package db

import (
	"context"
	"strings"
	"testing"
)

// TestExtensionerOnlyPostgres pins the optional-interface contract: Postgres
// supports extensions; MySQL/MariaDB/SQLite do not, so callers can offer the
// feature only where it exists.
func TestExtensionerOnlyPostgres(t *testing.T) {
	if _, ok := any(NewPostgres()).(Extensioner); !ok {
		t.Error("Postgres should implement Extensioner")
	}
	for _, e := range []Engine{NewMySQL(), NewMariaDB(), NewSQLite()} {
		if _, ok := any(e).(Extensioner); ok {
			t.Errorf("%T must not implement Extensioner", e)
		}
	}
}

// TestExtensionInstalled reflects installed state from the version column.
func TestExtensionInstalled(t *testing.T) {
	if (Extension{InstalledVersion: "3.4"}).Installed() != true {
		t.Error("a non-empty installed version means installed")
	}
	if (Extension{DefaultVersion: "3.4"}).Installed() != false {
		t.Error("available-but-not-installed must report false")
	}
}

// TestCreateExtensionValidatesName rejects a bad identifier before any server
// round-trip (an over-long name fails validation before the nil pool is touched).
func TestCreateExtensionValidatesName(t *testing.T) {
	p := NewPostgres()
	err := p.CreateExtension(context.Background(), "", strings.Repeat("a", 65))
	if err == nil {
		t.Fatal("expected an invalid-identifier error")
	}
}
