package main

import _ "embed"

//go:embed index.html
var indexHTML []byte

//go:embed help.html
var helpHTML []byte
