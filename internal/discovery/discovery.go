package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

type Node struct {
	NodeID   string
	Name     string
	Platform string
	Address  string
	Port     int
}

type Discovery struct {
	nodeID    string
	name      string
	platform  string
	port      int
	ifaceName string // optional: network interface name to use for mDNS

	mu    sync.RWMutex
	nodes map[string]Node

	server *zeroconf.Server
	cancel context.CancelFunc
}

func New(nodeID, name, platform string, port int, ifaceName string) *Discovery {
	return &Discovery{
		nodeID:    nodeID,
		name:      name,
		platform:  platform,
		port:      port,
		ifaceName: ifaceName,
		nodes:     make(map[string]Node),
	}
}

func (d *Discovery) Start(ctx context.Context) error {
	ctx, d.cancel = context.WithCancel(ctx)

	meta := []string{
		"id=" + d.nodeID,
		"name=" + d.name,
		"platform=" + d.platform,
	}

	var interfaces []net.Interface
	if d.ifaceName != "" {
		iface, err := net.InterfaceByName(d.ifaceName)
		if err != nil {
			log.Printf("mDNS: interface %s not found: %v, falling back to all", d.ifaceName, err)
		} else {
			interfaces = []net.Interface{*iface}
		}
	}

	server, err := zeroconf.Register(d.nodeID, "_pocketcluster._tcp", "local.", d.port, meta, interfaces)
	if err != nil {
		return fmt.Errorf("register mDNS: %w", err)
	}
	d.server = server

	go d.browse(ctx)
	return nil
}

func (d *Discovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.server != nil {
		d.server.Shutdown()
	}
}

func (d *Discovery) Nodes() []Node {
	d.mu.RLock()
	defer d.mu.RUnlock()
	nodes := make([]Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

func (d *Discovery) SetNodeOnline(nodeID, name, platform, address string, port int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nodes[nodeID] = Node{
		NodeID:   nodeID,
		Name:     name,
		Platform: platform,
		Address:  address,
		Port:     port,
	}
}

func (d *Discovery) RemoveNode(nodeID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.nodes, nodeID)
}

func (d *Discovery) browse(ctx context.Context) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("mDNS resolver error: %v", err)
		return
	}
	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-entries:
				if !ok {
					return
				}
				d.handleEntry(entry)
			}
		}
	}()
	for {
		// Recover from zeroconf panic on close
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("mDNS browse recovered from panic: %v", r)
				}
			}()
			if err := resolver.Browse(ctx, "_pocketcluster._tcp", "local.", entries); err != nil {
				log.Printf("mDNS browse error: %v", err)
			}
		}()
		select {
		case <-ctx.Done():
			return
		default:
			time.Sleep(10 * time.Second)
		}
	}
}

func (d *Discovery) handleEntry(entry *zeroconf.ServiceEntry) {
	if len(entry.AddrIPv4) == 0 {
		return
	}
	nodeID := ""
	name := ""
	platform := ""
	for _, txt := range entry.Text {
		if len(txt) > 3 && txt[:3] == "id=" {
			nodeID = txt[3:]
		}
		if len(txt) > 5 && txt[:5] == "name=" {
			name = txt[5:]
		}
		if len(txt) > 9 && txt[:9] == "platform=" {
			platform = txt[9:]
		}
	}
	if nodeID == "" || nodeID == d.nodeID {
		return
	}
	addr := entry.AddrIPv4[0].String()
	port := entry.Port
	d.SetNodeOnline(nodeID, name, platform, addr+":"+strconv.Itoa(port), port)
	log.Printf("mDNS discovered node: %s (%s) at %s:%d", name, nodeID, addr, port)
}
