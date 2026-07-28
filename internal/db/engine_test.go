package db

import (
	"context"
	"errors"
	"testing"
)

// stubEngine is a do-nothing Engine used to prove the interface is satisfiable
// and stable across later phases.
type stubEngine struct{}

func (stubEngine) Kind() Kind { return KindPostgres }
func (stubEngine) Capabilities() Capabilities {
	return Capabilities{Users: true, Grants: true, MultipleDatabases: true}
}

func (stubEngine) Connect(context.Context, Target) error { return nil }
func (stubEngine) Close() error                          { return nil }

func (stubEngine) ListDatabases(context.Context) ([]Database, error)        { return nil, nil }
func (stubEngine) CreateDatabase(context.Context, string, CreateOpts) error { return nil }
func (stubEngine) DropDatabase(context.Context, string) error               { return nil }

func (stubEngine) ListTables(context.Context, string) ([]Table, error) { return nil, nil }
func (stubEngine) DescribeTable(context.Context, string, string) ([]Column, error) {
	return nil, nil
}
func (stubEngine) PreviewRows(context.Context, string, string, int) (Result, error) {
	return Result{}, nil
}

func (stubEngine) ListUsers(context.Context) ([]User, error)                { return nil, nil }
func (stubEngine) CreateUser(context.Context, string, string) error         { return nil }
func (stubEngine) DropUser(context.Context, string) error                   { return nil }
func (stubEngine) Grant(context.Context, string, string, GrantLevel) error  { return nil }
func (stubEngine) Revoke(context.Context, string, string, GrantLevel) error { return nil }

func (stubEngine) AlterUser(context.Context, string, bool, bool) error { return nil }
func (stubEngine) SetPassword(context.Context, string, string) error   { return nil }

func (stubEngine) DatabasePrivileges() []Privilege { return nil }
func (stubEngine) ListGrants(context.Context, string, string) ([]Privilege, error) {
	return nil, nil
}
func (stubEngine) SetGrant(context.Context, string, string, Privilege, bool) error { return nil }

func (stubEngine) Query(context.Context, string) (Result, error) { return Result{}, nil }
func (stubEngine) ExecMutation(context.Context, string, string) (Result, error) {
	return Result{}, nil
}

// Compile-time assertion that stubEngine satisfies Engine.
var _ Engine = stubEngine{}

func TestClassifyScaffold(t *testing.T) {
	if classify(nil) != nil {
		t.Fatal("classify(nil) should return nil")
	}
	got := classify(errors.New("boom"))
	if got == nil || got.Kind != DBErrInternal {
		t.Fatalf("classify(err) = %+v, want kind DBErrInternal", got)
	}
	if !errors.Is(got, got.Err) {
		t.Fatal("DBError should unwrap to its wrapped error")
	}
}
