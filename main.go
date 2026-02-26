package main

import (
	_ "embed"

	"github.com/yjiky/linctl/cmd"
)

//go:embed README.md
var readmeContents string

func main() {
	cmd.SetReadmeContents(readmeContents)
	cmd.Execute()
}
