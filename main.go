package main

import (
	"fmt"
	"github.com/MetaSyntactical/voyager-traefik-manager/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.Execute(fmt.Sprintf("%s (%s) (built on %s)", version, commit, date))
}
