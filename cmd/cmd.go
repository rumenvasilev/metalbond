package main

import (
	"runtime"
	"time"

	"github.com/alecthomas/kong"
	"github.com/onmetal/metalbond"
	log "github.com/sirupsen/logrus"
)

var CLI struct {
	Server struct {
		Listen    string `help:"listen address. e.g. [::]:4711"`
		Verbose   bool   `help:"Enable debug logging" short:"v"`
		Keepalive uint32 `help:"Keepalive Interval"`
	} `cmd:"" help:"Run MetalBond Server"`

	Client struct {
		Server    []string `help:"Server address. You may define multiple servers."`
		Subscribe []uint32 `help:"Subscribe to VNIs"`
		Announce  []string `help:"Announce Prefixes in VNIs (e.g. 23#2001:db8::/64)"`
		Verbose   bool     `help:"Enable debug logging" short:"v"`
		Netlink   bool     `help:"install routes via netlink"`
		Keepalive uint32   `help:"Keepalive Interval"`
	} `cmd:"" help:"Run MetalBond Client"`
}

func main() {
	log.Infof("MetalBond")

	go func() {
		for {
			log.Debugf("Active Go Routines: %d", runtime.NumGoroutine())
			time.Sleep(time.Duration(60 * time.Second))
		}
	}()

	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "server":
		if CLI.Server.Verbose {
			log.SetLevel(log.DebugLevel)
		}

		serverConfig := metalbond.ServerConfig{
			ListenAddress:     CLI.Server.Listen,
			KeepaliveInterval: CLI.Server.Keepalive,
		}

		metalbond.NewServer(serverConfig)

	case "client":
		log.Infof("Client")
		log.Infof("  servers: %v", CLI.Client.Server)

		if CLI.Client.Verbose {
			log.SetLevel(log.DebugLevel)
		}

		clientConfig := metalbond.ClientConfig{
			Servers:           CLI.Client.Server,
			KeepaliveInterval: CLI.Client.Keepalive,
			Netlink:           CLI.Client.Netlink,
		}
		metalbond.NewClient(clientConfig)

	default:
		log.Errorf("Error: %v", ctx.Command())
	}
}
