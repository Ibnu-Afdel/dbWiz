package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// imageEngines maps a normalized image repository to the engine it runs.
// Classification is by repository only — never by container name or image tag
// (an app tagged ":postgresql-latest" is not a database). The list covers the
// common official images and their popular variants; see 01-RESEARCH.md §1–2.
var imageEngines = map[string]Engine{
	"postgres":              EnginePostgres,
	"pgvector/pgvector":     EnginePostgres,
	"postgis/postgis":       EnginePostgres,
	"timescale/timescaledb": EnginePostgres,
	"bitnami/postgresql":    EnginePostgres,
	"mysql":                 EngineMySQL,
	"percona":               EngineMySQL,
	"bitnami/mysql":         EngineMySQL,
	"mariadb":               EngineMariaDB,
}

// omarchyNames maps Omarchy's stock container names to the engine they must run
// for the name to count. Name alone never classifies an engine; this only tags
// the Source and attaches known default credentials when the image already
// classified to the same engine.
var omarchyNames = map[string]Engine{
	"postgres18": EnginePostgres,
	"mysql8":     EngineMySQL,
	"mariadb11":  EngineMariaDB,
}

// psLine mirrors the fields we consume from `docker ps -a --format json`
// (one JSON object per line).
type psLine struct {
	Image string `json:"Image"`
	Names string `json:"Names"`
	Ports string `json:"Ports"`
	State string `json:"State"`
}

// Detect scans local containers with `docker ps -a` and returns the database
// ones (running and stopped), classified by image and, on Omarchy machines,
// annotated with known default credentials. It does not inspect containers for
// credentials or exact port mappings — that is Inspect's job.
func Detect(ctx context.Context) ([]Container, error) {
	return detect(ctx, dockerCLI, onOmarchy())
}

func detect(ctx context.Context, run dockerRunner, omarchy bool) ([]Container, error) {
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, classifyRun("detect", ctx, err, stderr)
	}

	lines, err := parsePS(stdout)
	if err != nil {
		return nil, errInternal("parse", err)
	}

	var out []Container
	for _, l := range lines {
		eng := classifyImage(l.Image)
		if eng == EngineUnknown {
			continue
		}
		c := Container{
			Name:     firstName(l.Names),
			Image:    l.Image,
			Engine:   eng,
			State:    stateFromString(l.State),
			HostPort: parseHostPort(l.Ports, defaultPort(eng)),
		}
		if omarchy {
			if creds, ok := omarchyCreds(c.Name, eng); ok {
				c.Source = SourceOmarchy
				c.Creds = creds
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// parsePS decodes the newline-delimited JSON stream docker emits. Blank lines
// are skipped; a malformed line aborts with an error rather than guessing.
func parsePS(stdout []byte) ([]psLine, error) {
	var lines []psLine
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // labels blobs can be large
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var l psLine
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// classifyImage returns the engine an image runs, or EngineUnknown if its
// repository isn't a recognized database image.
func classifyImage(image string) Engine {
	return imageEngines[imageRepo(image)]
}

// imageRepo normalizes a docker image reference down to its repository, dropping
// any digest, tag, registry host, and the implicit docker.io "library/"
// namespace. e.g. "ghcr.io/umami-software/umami:postgresql-latest" ->
// "umami-software/umami"; "postgres:18" -> "postgres";
// "docker.io/library/mariadb:11" -> "mariadb".
func imageRepo(image string) string {
	if i := strings.IndexByte(image, '@'); i >= 0 { // strip digest
		image = image[:i]
	}
	// Strip the tag: the last ':' that has no '/' after it. A ':' followed by a
	// '/' is a registry port (host:5000/repo), not a tag separator.
	if i := strings.LastIndexByte(image, ':'); i >= 0 && !strings.Contains(image[i:], "/") {
		image = image[:i]
	}
	parts := strings.Split(image, "/")
	// Strip a registry host: the first segment when it looks like a hostname
	// (contains '.' or ':') or is localhost.
	if len(parts) > 1 {
		if first := parts[0]; first == "localhost" || strings.ContainsAny(first, ".:") {
			parts = parts[1:]
		}
	}
	// Strip docker's implicit library namespace (docker.io/library/postgres).
	if len(parts) > 1 && parts[0] == "library" {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}

// parseHostPort extracts the host-side port mapped to containerPort from a
// docker ps Ports field like "0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp".
// Returns 0 when there is no published mapping for that container port (the
// normal case for stopped containers).
func parseHostPort(ports string, containerPort int) int {
	if containerPort == 0 || ports == "" {
		return 0
	}
	for seg := range strings.SplitSeq(ports, ",") {
		seg = strings.TrimSpace(seg)
		left, right, ok := strings.Cut(seg, "->")
		if !ok { // exposed-only ("5432/tcp"), not published
			continue
		}
		cp := right // "5432/tcp"
		if i := strings.IndexByte(cp, '/'); i >= 0 {
			cp = cp[:i]
		}
		if cp != strconv.Itoa(containerPort) {
			continue
		}
		host := left
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			host = host[i+1:]
		}
		if hp, err := strconv.Atoi(host); err == nil {
			return hp
		}
	}
	return 0
}

// omarchyCreds returns the known default credentials for an Omarchy stock
// container, but only when the name matches an engine that agrees with the
// image-derived classification. Postgres is trust-auth (user postgres, no
// password); MySQL/MariaDB have an empty-password root.
func omarchyCreds(name string, eng Engine) (Creds, bool) {
	if want, ok := omarchyNames[name]; !ok || want != eng {
		return Creds{}, false
	}
	switch eng {
	case EnginePostgres:
		return Creds{User: "postgres", Database: "postgres"}, true
	case EngineMySQL, EngineMariaDB:
		return Creds{User: "root"}, true
	}
	return Creds{}, false
}

// firstName takes the first name from docker's comma-joined Names field.
func firstName(names string) string {
	first, _, _ := strings.Cut(names, ",")
	return first
}

// stateFromString maps docker's State string to our two-value state. Only
// "running" is Up; everything else (exited, created, paused, dead, …) is
// treated as stopped for the picker.
func stateFromString(state string) ContainerState {
	if strings.EqualFold(state, "running") {
		return StateRunning
	}
	return StateStopped
}
