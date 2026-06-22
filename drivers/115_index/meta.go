package _115_index

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootID
}

var config = driver.Config{
	Name:        "115 Index",
	DefaultRoot: "",
	NoUpload:    true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Pan115Index{}
	})
}
