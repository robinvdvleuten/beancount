package beancount

import (
	"github.com/alecthomas/kong"
	"github.com/alecthomas/repr"
)

type CheckCmd struct {
	File []byte `help:"Beancount input filename." arg:"" type:"filecontent"`
}

func (cmd *CheckCmd) Run(ctx *kong.Context) error {
	b, err := ParseBytes(cmd.File)
	if err != nil {
		return err
	}

	repr.Println(b)

	return nil
}

type Commands struct {
	Check CheckCmd `cmd:"" help:"Parse, check and realize a beancount input file."`
}
