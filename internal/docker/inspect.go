package docker

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// inspectData mirrors the fields we consume from `docker inspect <name>`, which
// returns a JSON array with a single object.
type inspectData struct {
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]portBinding `json:"Ports"`
	} `json:"NetworkSettings"`
	HostConfig struct {
		PortBindings map[string][]portBinding `json:"PortBindings"`
	} `json:"HostConfig"`
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// Inspect recovers a container's host-side port mapping and credentials via
// `docker inspect`. Any field that can't be determined comes back zero/empty —
// this is a best-effort enrichment step, not a hard requirement (a caller that
// already has Omarchy defaults may skip it).
func Inspect(ctx context.Context, name string, eng Engine) (hostPort int, creds Creds, err error) {
	return inspect(ctx, dockerCLI, name, eng)
}

// InspectRemote is Inspect against a remote Docker daemon over SSH (v3 3.3).
// dockerHost is a DOCKER_HOST value like "ssh://user@host:port".
func InspectRemote(ctx context.Context, dockerHost, name string, eng Engine) (int, Creds, error) {
	return inspectWithin(ctx, remoteCLI(dockerHost), name, eng, remoteOpTimeout)
}

func inspect(ctx context.Context, run dockerRunner, name string, eng Engine) (int, Creds, error) {
	return inspectWithin(ctx, run, name, eng, detectTimeout)
}

func inspectWithin(ctx context.Context, run dockerRunner, name string, eng Engine, timeout time.Duration) (int, Creds, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout, stderr, err := run(ctx, "inspect", name)
	if err != nil {
		return 0, Creds{}, classifyRun("inspect", ctx, err, stderr)
	}

	data, err := parseInspect(stdout)
	if err != nil {
		return 0, Creds{}, errInternal("parse", err)
	}
	return hostPortFromInspect(data, defaultPort(eng)), credsFromEnv(data.Config.Env, eng), nil
}

// parseInspect decodes the single-element array docker inspect returns.
func parseInspect(stdout []byte) (inspectData, error) {
	var arr []inspectData
	if err := json.Unmarshal(stdout, &arr); err != nil {
		return inspectData{}, err
	}
	if len(arr) == 0 {
		return inspectData{}, nil
	}
	return arr[0], nil
}

// hostPortFromInspect finds the host port bound to containerPort. It prefers the
// live NetworkSettings.Ports (populated while running) and falls back to the
// configured HostConfig.PortBindings (present even while stopped).
func hostPortFromInspect(d inspectData, containerPort int) int {
	key := strconv.Itoa(containerPort) + "/tcp"
	if p := pickHostPort(d.NetworkSettings.Ports[key]); p != 0 {
		return p
	}
	return pickHostPort(d.HostConfig.PortBindings[key])
}

// pickHostPort returns the first parseable host port from a binding list,
// preferring IPv4/all-interfaces bindings over IPv6-only ("::") ones.
func pickHostPort(bindings []portBinding) int {
	var v6 int
	for _, b := range bindings {
		hp, err := strconv.Atoi(b.HostPort)
		if err != nil || hp == 0 {
			continue
		}
		if b.HostIP == "::" {
			v6 = hp
			continue
		}
		return hp
	}
	return v6
}

// credsFromEnv recovers credentials from a container's environment variables —
// the standard official-image knobs (POSTGRES_*, MYSQL_*, MARIADB_*). For
// MySQL/MariaDB it prefers root (the admin account v1 needs) and falls back to
// the app user. Missing values stay empty.
func credsFromEnv(env []string, eng Engine) Creds {
	m := envMap(env)
	switch eng {
	case EnginePostgres:
		user := firstNonEmpty(m["POSTGRES_USER"], "postgres")
		return Creds{
			User:     user,
			Password: m["POSTGRES_PASSWORD"],
			Database: firstNonEmpty(m["POSTGRES_DB"], user),
		}
	case EngineMySQL, EngineMariaDB:
		rootPass, hasRoot := lookup(m, "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD")
		_, allowEmpty := lookup(m, "MYSQL_ALLOW_EMPTY_PASSWORD", "MARIADB_ALLOW_EMPTY_ROOT_PASSWORD", "ALLOW_EMPTY_PASSWORD")
		db, _ := lookup(m, "MYSQL_DATABASE", "MARIADB_DATABASE")
		if hasRoot || allowEmpty {
			return Creds{User: "root", Password: rootPass, Database: db}
		}
		user, _ := lookup(m, "MYSQL_USER", "MARIADB_USER")
		pass, _ := lookup(m, "MYSQL_PASSWORD", "MARIADB_PASSWORD")
		return Creds{User: user, Password: pass, Database: db}
	}
	return Creds{}
}

// envMap splits docker's "KEY=VALUE" env entries into a map. An entry without
// '=' maps to an empty value.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

// lookup returns the first present key's value (present may be empty string).
func lookup(m map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v, true
		}
	}
	return "", false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
