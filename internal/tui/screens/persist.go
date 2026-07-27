package screens

import (
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// remember* record the just-connected target in the cross-session state cache so
// the next launch can offer to continue here (v2 Step 1.2). They are strictly
// best-effort: the write is fire-and-forget and its error is dropped, because a
// caching hiccup must never interrupt actually connecting to a database.

func rememberDocker(c docker.Container) {
	_ = state.SetLastDocker(c.Name, string(c.Engine))
}

func rememberSQLite(path string) {
	_ = state.SetLastSQLite(path)
}
