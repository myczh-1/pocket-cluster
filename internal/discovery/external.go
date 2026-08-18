package discovery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
)

const maxExternalEventSize = 64 * 1024

type externalEvent struct {
	Op       string `json:"op"`
	NodeID   string `json:"node_id"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	IP       string `json:"ip,omitempty"`
	Port     int    `json:"port,omitempty"`
}

// StartExternal accepts newline-delimited discovery events from a trusted
// parent process. Android uses this path because its platform NSD service can
// access mDNS reliably while a sandboxed native child cannot enumerate network
// interfaces on recent Android versions.
func (d *Discovery) StartExternal(ctx context.Context, reader io.Reader) error {
	if reader == nil {
		return fmt.Errorf("external discovery reader is nil")
	}
	ctx, d.cancel = context.WithCancel(ctx)
	go d.consumeExternalEvents(ctx, reader)
	return nil
}

func (d *Discovery) consumeExternalEvents(ctx context.Context, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxExternalEventSize)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var event externalEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			log.Printf("external discovery: invalid event: %v", err)
			continue
		}
		if err := d.applyExternalEvent(event); err != nil {
			log.Printf("external discovery: rejected event: %v", err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("external discovery: read failed: %v", err)
	}
}

func (d *Discovery) applyExternalEvent(event externalEvent) error {
	if event.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}

	switch event.Op {
	case "upsert":
		ip := net.ParseIP(event.IP)
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
			return fmt.Errorf("invalid node IP %q", event.IP)
		}
		if event.Port < 1 || event.Port > 65535 {
			return fmt.Errorf("invalid node port %d", event.Port)
		}
		address := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", event.Port))
		d.SetNodeOnline(event.NodeID, event.Name, event.Platform, address, event.Port)
		log.Printf("external discovery: discovered node %s (%s) at %s", event.Name, event.NodeID, address)
		return nil
	case "remove":
		d.RemoveNode(event.NodeID)
		log.Printf("external discovery: removed node %s", event.NodeID)
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", event.Op)
	}
}
