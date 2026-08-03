package screens

import "github.com/Ibnu-Afdel/dbwiz/internal/keymap"

// The bindings themselves live in internal/keymap, a leaf package, so that the
// `dbwiz keys` command can read exactly what the handlers here match on without
// the CLI having to import the tui. These aliases keep every existing
// `Keys.Run`-style reference in this package working unchanged.
type KeyMap = keymap.KeyMap

// Keys is the single instance every screen and the root model share.
var Keys = keymap.Keys
