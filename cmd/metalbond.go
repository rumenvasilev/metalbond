package main

import (
	"runtime"
	"time"

	"github.com/alecthomas/kong"
	"github.com/google/uuid"
	"github.com/onmetal/metalbond"
	log "github.com/sirupsen/logrus"
)

var CLI struct {
	Server struct {
		Listen    string `help:"listen address. e.g. [::]:4711"`
		NodeUUID  string `help:"Node UUID"`
		Hostname  string `help:"Hostname"`
		Verbose   bool   `help:"Enable debug logging" short:"v"`
		Keepalive uint32 `help:"Keepalive Interval"`
	} `cmd:"" help:"Run MetalBond Server"`

	Client struct {
		NodeUUID  string   `help:"Node UUID"`
		Hostname  string   `help:"Hostname"`
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
			NodeUUID:          uuid.MustParse(CLI.Server.NodeUUID),
			Hostname:          CLI.Server.Hostname,
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
			NodeUUID:          uuid.MustParse((CLI.Client.NodeUUID)),
			Hostname:          CLI.Client.Hostname,
			KeepaliveInterval: CLI.Client.Keepalive,
		}
		metalbond.NewClient(clientConfig)

	default:
		log.Errorf("Error: %v", ctx.Command())
	}
}
