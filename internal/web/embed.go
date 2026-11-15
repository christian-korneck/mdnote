package web

import "embed"

// StaticFS embeds the minimal HTML, CSS, and JS files for the mdnote web interface.
//
//go:embed static/*
var StaticFS embed.FS
