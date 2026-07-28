package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// fakeExt is a stand-in Extensioner: it serves a fixed availability list and
// records installs, so the command's logic is tested without a live Postgres.
type fakeExt struct {
	avail   []db.Extension
	created []string
	listErr error
}

func (f *fakeExt) ListExtensions(context.Context, string) ([]db.Extension, error) {
	return f.avail, f.listErr
}
func (f *fakeExt) CreateExtension(_ context.Context, _ string, name string) error {
	f.created = append(f.created, name)
	return nil
}

func sampleExts() []db.Extension {
	return []db.Extension{
		{Name: "plpgsql", DefaultVersion: "1.0", InstalledVersion: "1.0", Comment: "PL/pgSQL"},
		{Name: "uuid-ossp", DefaultVersion: "1.1", Comment: "UUID generation"},
	}
}

// TestExtListShowsInstalledMarker renders the availability table with versions.
func TestExtListShowsInstalledMarker(t *testing.T) {
	f := &fakeExt{avail: sampleExts()}
	var out bytes.Buffer
	if err := runExtList(context.Background(), &out, f, "app", false); err != nil {
		t.Fatalf("runExtList: %v", err)
	}
	s := out.String()
	for _, must := range []string{"NAME", "plpgsql", "uuid-ossp", "1.0", "UUID generation"} {
		if !strings.Contains(s, must) {
			t.Errorf("list missing %q:\n%s", must, s)
		}
	}
}

// TestExtListInstalledOnly filters out the available-but-not-installed rows.
func TestExtListInstalledOnly(t *testing.T) {
	f := &fakeExt{avail: sampleExts()}
	var out bytes.Buffer
	if err := runExtList(context.Background(), &out, f, "app", true); err != nil {
		t.Fatalf("runExtList: %v", err)
	}
	if strings.Contains(out.String(), "uuid-ossp") {
		t.Errorf("installed-only should hide uuid-ossp:\n%s", out.String())
	}
}

// TestExtCreateInstallsAvailable installs an extension the image provides.
func TestExtCreateInstallsAvailable(t *testing.T) {
	f := &fakeExt{avail: sampleExts()}
	var out bytes.Buffer
	if err := runExtCreate(context.Background(), &out, f, "app", "uuid-ossp"); err != nil {
		t.Fatalf("runExtCreate: %v", err)
	}
	if len(f.created) != 1 || f.created[0] != "uuid-ossp" {
		t.Errorf("expected uuid-ossp installed, got %v", f.created)
	}
	if !strings.Contains(out.String(), "Installed extension uuid-ossp") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

// TestExtCreateAlreadyInstalled is a no-op with a clear message.
func TestExtCreateAlreadyInstalled(t *testing.T) {
	f := &fakeExt{avail: sampleExts()}
	var out bytes.Buffer
	if err := runExtCreate(context.Background(), &out, f, "app", "plpgsql"); err != nil {
		t.Fatalf("runExtCreate: %v", err)
	}
	if len(f.created) != 0 {
		t.Error("must not re-create an already-installed extension")
	}
	if !strings.Contains(out.String(), "already installed") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

// TestExtCreateUnavailablePostgis explains the image can't provide postgis and
// points at `dbwiz setup postgis` — the image-capability awareness.
func TestExtCreateUnavailablePostgis(t *testing.T) {
	f := &fakeExt{avail: sampleExts()} // no postgis in a plain image
	var out bytes.Buffer
	err := runExtCreate(context.Background(), &out, f, "app", "postgis")
	if err == nil || !strings.Contains(err.Error(), "setup postgis") {
		t.Fatalf("expected a capability explanation pointing at setup, got %v", err)
	}
	if len(f.created) != 0 {
		t.Error("must not attempt to create an unavailable extension")
	}
}
