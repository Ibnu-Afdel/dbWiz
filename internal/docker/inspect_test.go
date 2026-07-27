package docker

import (
	"context"
	"testing"
)

// pgInspect mirrors a real `docker inspect` (trimmed) for a running pgvector
// container with credentials in Config.Env and both live and configured port
// bindings.
const pgInspect = `[
  {
    "Config": {
      "Env": ["POSTGRES_DB=fawz", "POSTGRES_USER=fawz", "POSTGRES_PASSWORD=secret", "PGDATA=/var/lib/postgresql/data"]
    },
    "NetworkSettings": {
      "Ports": {"5432/tcp": [{"HostIp": "0.0.0.0", "HostPort": "5433"}, {"HostIp": "::", "HostPort": "5433"}]}
    },
    "HostConfig": {
      "PortBindings": {"5432/tcp": [{"HostIp": "", "HostPort": "5433"}]}
    }
  }
]`

func TestInspectPostgres(t *testing.T) {
	port, creds, err := inspect(context.Background(), fakeRunner(pgInspect, "", nil), "fawz-postgres", EnginePostgres)
	if err != nil {
		t.Fatal(err)
	}
	if port != 5433 {
		t.Errorf("port = %d, want 5433", port)
	}
	want := Creds{User: "fawz", Password: "secret", Database: "fawz"}
	if creds != want {
		t.Errorf("creds = %+v, want %+v", creds, want)
	}
}

func TestCredsFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		eng  Engine
		want Creds
	}{
		{
			"postgres defaults when unset",
			[]string{"POSTGRES_PASSWORD=pw"},
			EnginePostgres,
			Creds{User: "postgres", Password: "pw", Database: "postgres"},
		},
		{
			"mysql prefers root",
			[]string{"MYSQL_ROOT_PASSWORD=rootpw", "MYSQL_USER=app", "MYSQL_PASSWORD=apppw", "MYSQL_DATABASE=shop"},
			EngineMySQL,
			Creds{User: "root", Password: "rootpw", Database: "shop"},
		},
		{
			"mysql falls back to app user when no root",
			[]string{"MYSQL_USER=app", "MYSQL_PASSWORD=apppw", "MYSQL_DATABASE=shop"},
			EngineMySQL,
			Creds{User: "app", Password: "apppw", Database: "shop"},
		},
		{
			"mysql empty root via ALLOW_EMPTY_PASSWORD",
			[]string{"MYSQL_ALLOW_EMPTY_PASSWORD=true", "MYSQL_DATABASE=shop"},
			EngineMySQL,
			Creds{User: "root", Password: "", Database: "shop"},
		},
		{
			"mariadb env aliases",
			[]string{"MARIADB_ROOT_PASSWORD=rp", "MARIADB_DATABASE=d"},
			EngineMariaDB,
			Creds{User: "root", Password: "rp", Database: "d"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := credsFromEnv(tt.env, tt.eng); got != tt.want {
				t.Errorf("credsFromEnv = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHostPortFromInspectFallsBackToConfig(t *testing.T) {
	// Stopped container: no live NetworkSettings.Ports, only configured bindings.
	stopped := `[{"Config":{"Env":[]},"NetworkSettings":{"Ports":{}},"HostConfig":{"PortBindings":{"3306/tcp":[{"HostIp":"127.0.0.1","HostPort":"3306"}]}}}]`
	port, _, err := inspect(context.Background(), fakeRunner(stopped, "", nil), "mysql8", EngineMySQL)
	if err != nil {
		t.Fatal(err)
	}
	if port != 3306 {
		t.Errorf("port = %d, want 3306 (from HostConfig fallback)", port)
	}
}
