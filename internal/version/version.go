// Copyright (c) 2026 Kevin Herro
// SPDX-License-Identifier: MIT

package version

import "runtime"

var (
	// Version is replaced by release builds with -ldflags -X.
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}
