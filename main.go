package main

import (
	"embed"
	"fmt"
	"os"
	"strings"

	donksrc "donk/src"
)

const (
	usageText = `donk is a lightweight cli for syncing development configs and libraries with an OSS-backed source.

donk usage:
  donk init
  donk cfg pull <name>
  donk cfg push <name>
  donk cfg init <name>
  donk lib pull <name>
  donk help`

	cfgHelpText = `USAGE:
  donk cfg pull <name>
  donk cfg push <name>
  donk cfg init <name>

EXAMPLES:
  donk cfg push nvim
  donk cfg pull nvim
  donk cfg init nvim`

	libHelpText = `USAGE:
  donk lib pull <name>

EXAMPLE:
  donk lib pull zulu-jdk-8`

	initHelpText = `USAGE:
  donk init`
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
		fmt.Println(usageText)
		return nil
	}

	initCmd, err := donksrc.CreateInitCmd(embeddedFiles)
	if err != nil {
		return err
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Println(usageText)
		return nil
	case "init":
		if isHelpArg(args, 1) {
			fmt.Println(initHelpText)
			return nil
		}
		return initCmd.Run(args)
	case "cfg":
		if isHelpArg(args, 1) {
			fmt.Println(cfgHelpText)
			return nil
		}
		context, err := initCmd.LoadContext()
		if err != nil {
			return err
		}
		return donksrc.CreateCfgCmd(context).Run(args)
	case "lib":
		if isHelpArg(args, 1) {
			fmt.Println(libHelpText)
			return nil
		}
		context, err := initCmd.LoadContext()
		if err != nil {
			return err
		}
		return donksrc.CreateLibCmd(context).Run(args)
	default:
		return fmt.Errorf("unknown command: %s\n\n%s", args[0], usageText)
	}
}

func isHelpArg(args []string, idx int) bool {
	if len(args) <= idx {
		return false
	}
	value := strings.TrimSpace(args[idx])
	return value == "help" || value == "-h" || value == "--help"
}
