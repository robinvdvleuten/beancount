package beancount

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/alecthomas/repr"
)

type CheckCmd struct {
	Filename string `help:"Beancount input filename." arg:"" type:"existingfile"`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	raw, err := os.ReadFile(cmd.Filename)
	if err != nil {
		return err
	}

	b, err := ParseBytes(raw)
	if err != nil {
		return err
	}

	repr.Println(b)

	return nil
}

type Commands struct {
	Check CheckCmd `cmd:"" help:"Parse, check and realize a beancount input file."`
}
