package main

import (
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/onmetal/metalbond"
	log "github.com/sirupsen/logrus"
	"inet.af/netaddr"
)

var CLI struct {
	Server struct {
		Listen    string `help:"listen address. e.g. [::]:4711"`
		Verbose   bool   `help:"Enable debug logging" short:"v"`
		Keepalive uint32 `help:"Keepalive Interval"`
	} `cmd:"" help:"Run MetalBond Server"`

	Client struct {
		Server        []string `help:"Server address. You may define multiple servers."`
		Subscribe     []uint32 `help:"Subscribe to VNIs"`
		Announce      []string `help:"Announce Prefixes in VNIs (e.g. 23#2001:db8:cafe::/64#2001:db8::cafe)"`
		Verbose       bool     `help:"Enable debug logging" short:"v"`
		InstallRoutes bool     `help:"install routes via netlink"`
		Link          string   `help:"ip6tnl link name"`
		RouteTable    int      `help:"install routes into a specified table (e.g. when routes should be installed into a VRF)"`
		Keepalive     uint32   `help:"Keepalive Interval"`
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
		if CLI.Client.InstallRoutes {
			if err := m.EnableNetlink(CLI.Client.Link, CLI.Client.RouteTable); err != nil {
				log.Fatalf("Cannot enable netlink: %v", err)
			}
		}
		for _, server := range CLI.Client.Server {
			m.AddPeer(server, metalbond.OUTGOING)
		}

		for _, announcement := range CLI.Client.Announce {
			parts := strings.Split(announcement, "#")
			if len(parts) != 3 {
				log.Fatalf("malformed announcement: %s", announcement)
			}

			vni, err := strconv.ParseInt(parts[0], 10, 24)
			if len(parts) != 3 {
				log.Fatalf("invalid VNI: %s", parts[0])
			}

			prefix, err := netaddr.ParseIPPrefix(parts[1])
			if err != nil {
				log.Fatalf("invalid prefix: %s", parts[1])
			}

			var ipversion metalbond.IPVersion
			if prefix.IP().Is4() {
				ipversion = metalbond.IPV4
			} else {
				ipversion = metalbond.IPV6
			}

			dest := metalbond.Destination{
				IPVersion: ipversion,
				Prefix:    prefix,
			}

			hopIP, err := netaddr.ParseIP(parts[2])
			if err != nil {
				log.Fatalf("invalid nexthop address: %s - %v", parts[2], err)
			}

			hop := metalbond.NextHop{
				TargetAddress: hopIP,
				TargetVNI:     0,
				NAT:           false,
			}

			m.AnnounceRoute(uint(vni), dest, hop)
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
