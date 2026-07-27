// Package docker discovers local database containers by shelling out to the
// docker CLI and classifying the results. It is pure logic plus process/network
// I/O and must never import the tui package.
//
// docker classifies a container's engine into its own Engine label (derived
// from the image name) rather than depending on the db package's Kind. This
// keeps detection self-contained and independently testable; the tui layer maps
// docker.Engine to db.Kind at the boundary.
package docker

// Engine is the database engine a container runs, as detected from its image.
type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineMySQL    Engine = "mysql"
	EngineMariaDB  Engine = "mariadb"
	EngineUnknown  Engine = ""
)

// ContainerState is whether a container is currently running.
type ContainerState int

const (
	StateStopped ContainerState = iota
	StateRunning
)

// Source distinguishes containers created by Omarchy's conventions from
// generic ones, which affects how confidently we recover credentials.
type Source int

const (
	SourceGeneric Source = iota
	SourceOmarchy
)

// Creds holds credentials recovered from a container's environment. Any field
// may be empty when it could not be determined.
type Creds struct {
	User     string
	Password string
	Database string
}

// Container is a detected database container.
type Container struct {
	Name     string
	Image    string
	Engine   Engine
	HostPort int // host-side port mapped to the engine's default port; 0 if none
	State    ContainerState
	Source   Source
	Creds    Creds
}

// defaultPort returns the engine's canonical container-side port, used to pick
// the right host-port mapping out of docker's port list.
func defaultPort(e Engine) int {
	switch e {
	case EnginePostgres:
		return 5432
	case EngineMySQL, EngineMariaDB:
		return 3306
	}
	return 0
}
