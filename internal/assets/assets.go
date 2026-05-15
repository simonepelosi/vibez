// Package assets embeds the vibez icon and .desktop file and provides
// helpers to install them into the user's local share directories.
package assets

import (
	_ "embed"
)

//go:embed vibez.svg
var Icon []byte

//go:embed vibez.desktop
var DesktopEntry []byte
