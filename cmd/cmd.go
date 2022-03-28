package main

import (
	"os"
	"os/signal"
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
			time.Sleep(time.Duration(10 * time.Second))
		}
	}()

	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "server":
		if CLI.Server.Verbose {
			log.SetLevel(log.DebugLevel)
		}

		m := metalbond.NewMetalBond(CLI.Server.Keepalive)
		m.StartServer(CLI.Server.Listen)

		// Wait for SIGINTs
		cint := make(chan os.Signal, 1)
		signal.Notify(cint, os.Interrupt)
		<-cint

		m.Shutdown()

	case "client":
		log.Infof("Client")
		log.Infof("  servers: %v", CLI.Client.Server)

		if CLI.Client.Verbose {
			log.SetLevel(log.DebugLevel)
		}

		m := metalbond.NewMetalBond(CLI.Client.Keepalive)
		if CLI.Client.Netlink {
			if err := m.EnableNetlink(); err != nil {
				log.Fatalf("Cannot enable netlink: %v", err)
			}
		}
		for _, server := range CLI.Client.Server {
			m.AddPeer(server, metalbond.OUTGOING)
		}

		// Wait for SIGINTs
		cint := make(chan os.Signal, 1)
		signal.Notify(cint, os.Interrupt)
		<-cint

		m.Shutdown()

	default:
		log.Errorf("Error: %v", ctx.Command())
	}
}
