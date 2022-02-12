package main

import (
	"github.com/alecthomas/kong"
	log "github.com/sirupsen/logrus"
)

var CLI struct {
	Server struct {
		Listen   string `help:"listen address. e.g. [::]:1337"`
		NodeUUID string `help:"Node UUID"`
		Hostname string `help:"Hostname"`
	} `cmd:"" help:"Run MetalBond Server"`

	Client struct {
		NodeUUID string   `help:"Node UUID"`
		Hostname string   `help:"Hostname"`
		Server   []string `help:"Server address. You may define multiple servers."`
	} `cmd:"" help:"Run MetalBond Client"`
}

func main() {
	log.Infof("MetalBond")

	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "server":
		log.Infof("Server")
	case "client":
		log.Infof("Client")
		log.Infof("  servers: %v", CLI.Client.Server)
	default:
		log.Errorf("Error: %v", ctx.Command())
	}
}
