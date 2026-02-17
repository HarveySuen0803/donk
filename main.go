package main

import (
	"embed"
	"fmt"
	"os"

	donksrc "donk/src"
)

const (
	usageText = `usage:
		donk init
		donk cfg pull <name>
		donk cfg push <name>
		donk lib pull <name>`
)

//go:embed settings.json
var embeddedFiles embed.FS

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("invalid command arguments. %s", usageText)
	}

	initCmd, err := donksrc.CreateInitCmd(embeddedFiles)
	if err != nil {
		return err
	}
	context, err := initCmd.LoadContext()
	if err != nil {
		return err
	}

	switch args[0] {
	case "init":
		return initCmd.Run(args)
	case "cfg":
		return donksrc.CreateCfgCmd(context).Run(args)
	case "lib":
		return donksrc.CreateLibCmd(context).Run(args)
	default:
		return fmt.Errorf("invalid command arguments. %s", usageText)
	}
}
