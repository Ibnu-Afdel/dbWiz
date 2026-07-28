package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// fakeAdmin records the admin calls runCreate/runDrop make and can be told to
// fail a given step, so the command logic is testable without a live database.
type fakeAdmin struct {
	caps       db.Capabilities
	createdDB  string
	createdOwn string
	createdUsr string
	usrPass    string
	granted    [3]string // user, database, ""(level)
	dropped    string
	failOn     string // "user" | "db" | "grant" | "drop" | ""
}

func (f *fakeAdmin) Capabilities() db.Capabilities { return f.caps }

func (f *fakeAdmin) CreateDatabase(_ context.Context, name string, opts db.CreateOpts) error {
	if f.failOn == "db" {
		return &db.DBError{Kind: db.DBErrObjectExists, Title: "Database exists", Detail: name + " is already there", Hint: "pick another name"}
	}
	f.createdDB, f.createdOwn = name, opts.Owner
	return nil
}

func (f *fakeAdmin) CreateUser(_ context.Context, name, password string) error {
	if f.failOn == "user" {
		return errors.New("boom user")
	}
	f.createdUsr, f.usrPass = name, password
	return nil
}

func (f *fakeAdmin) Grant(_ context.Context, user, database string, _ db.GrantLevel) error {
	if f.failOn == "grant" {
		return errors.New("boom grant")
	}
	f.granted = [3]string{user, database, ""}
	return nil
}

func (f *fakeAdmin) DropDatabase(_ context.Context, name string) error {
	if f.failOn == "drop" {
		return &db.DBError{Kind: db.DBErrObjectInUse, Title: "Database in use", Detail: "clients are connected", Hint: "disconnect them"}
	}
	f.dropped = name
	return nil
}

// TestCreatePlain creates just the database, no user, engine-default owner.
func TestCreatePlain(t *testing.T) {
	f := &fakeAdmin{caps: db.Capabilities{Users: true}}
	var out bytes.Buffer
	if err := runCreate(context.Background(), &out, f, "shop", false, ""); err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	if f.createdDB != "shop" || f.createdOwn != "" || f.createdUsr != "" {
		t.Fatalf("unexpected calls: %+v", f)
	}
	if !strings.Contains(out.String(), "Created database shop") {
		t.Errorf("missing confirmation: %q", out.String())
	}
}

// TestCreateWithUser does the one-shot user→db→grant flow in order.
func TestCreateWithUser(t *testing.T) {
	f := &fakeAdmin{caps: db.Capabilities{Users: true}}
	var out bytes.Buffer
	if err := runCreate(context.Background(), &out, f, "shop", true, "pw"); err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	if f.createdUsr != "shop" || f.usrPass != "pw" {
		t.Errorf("user not created with password: %+v", f)
	}
	if f.createdDB != "shop" || f.createdOwn != "shop" {
		t.Errorf("db not owned by new user: %+v", f)
	}
	if f.granted != [3]string{"shop", "shop", ""} {
		t.Errorf("grant not applied: %+v", f.granted)
	}
	if !strings.Contains(out.String(), "granted ALL") {
		t.Errorf("missing confirmation: %q", out.String())
	}
}

// TestCreateUserUnsupported rejects --user on an engine without users (SQLite)
// before touching the database.
func TestCreateUserUnsupported(t *testing.T) {
	f := &fakeAdmin{caps: db.Capabilities{Users: false}}
	err := runCreate(context.Background(), new(bytes.Buffer), f, "shop", true, "")
	if err == nil || !strings.Contains(err.Error(), "no users") {
		t.Fatalf("expected an unsupported-users error, got %v", err)
	}
	if f.createdUsr != "" || f.createdDB != "" {
		t.Errorf("nothing should have been created: %+v", f)
	}
}

// TestCreateInvalidName is rejected up front without any driver call.
func TestCreateInvalidName(t *testing.T) {
	f := &fakeAdmin{caps: db.Capabilities{Users: true}}
	// A control character can't be a real identifier on any engine — validated out
	// before any driver call (a space or punctuation, by contrast, is a legal
	// quoted name, so those aren't rejected here).
	if err := runCreate(context.Background(), new(bytes.Buffer), f, "bad\tname", false, ""); err == nil {
		t.Fatal("expected an invalid-identifier error")
	}
	if f.createdDB != "" {
		t.Error("no database should have been created for an invalid name")
	}
}

// TestCreateErrorKeepsDetail surfaces a DBError's Detail and Hint (which its own
// Error() drops).
func TestCreateErrorKeepsDetail(t *testing.T) {
	f := &fakeAdmin{caps: db.Capabilities{Users: true}, failOn: "db"}
	err := runCreate(context.Background(), new(bytes.Buffer), f, "shop", false, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"already there", "pick another name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should keep DBError detail/hint %q, got: %v", want, err)
		}
	}
}

// TestDrop drops the named database.
func TestDrop(t *testing.T) {
	f := &fakeAdmin{}
	var out bytes.Buffer
	if err := runDrop(context.Background(), &out, f, "shop"); err != nil {
		t.Fatalf("runDrop: %v", err)
	}
	if f.dropped != "shop" || !strings.Contains(out.String(), "Dropped database shop") {
		t.Fatalf("drop not confirmed: %+v / %q", f, out.String())
	}
}

// TestDropInUseKeepsDetail renders the typed in-use error plainly.
func TestDropInUseKeepsDetail(t *testing.T) {
	f := &fakeAdmin{failOn: "drop"}
	err := runDrop(context.Background(), new(bytes.Buffer), f, "shop")
	if err == nil || !strings.Contains(err.Error(), "clients are connected") {
		t.Fatalf("expected the in-use detail, got %v", err)
	}
}
