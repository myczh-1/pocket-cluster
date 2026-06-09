package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const doctorTimeout = 5 * time.Second

type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, warn, fail
	Message string `json:"message"`
}

func runDoctor(dataDir string, port int) {
	fmt.Println("PocketCluster Doctor")
	fmt.Println("====================")
	fmt.Println()

	var results []checkResult

	results = append(results, checkDataDir(dataDir))
	results = append(results, checkPort(port))
	results = append(results, checkAgentRunning(port))
	results = append(results, checkMDNS())
	results = append(results, checkWebDAV(port))
	results = append(results, checkNodeConnectivity(port))
	results = append(results, checkStorageWritable(dataDir))

	okCount, warnCount, failCount := 0, 0, 0
	for _, r := range results {
		icon := "✓"
		switch r.Status {
		case "warn":
			icon = "⚠"
			warnCount++
		case "fail":
			icon = "✗"
			failCount++
		default:
			okCount++
		}
		fmt.Printf("  %s %-30s %s\n", icon, r.Name, r.Message)
	}

	fmt.Println()
	fmt.Printf("Result: %d ok, %d warnings, %d failures\n", okCount, warnCount, failCount)
	if failCount > 0 {
		os.Exit(1)
	}
}

func checkDataDir(dataDir string) checkResult {
	info, err := os.Stat(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return checkResult{"Data directory", "warn", fmt.Sprintf("%s does not exist (will be created on first run)", dataDir)}
		}
		return checkResult{"Data directory", "fail", err.Error()}
	}
	if !info.IsDir() {
		return checkResult{"Data directory", "fail", fmt.Sprintf("%s is not a directory", dataDir)}
	}
	dbPath := filepath.Join(dataDir, "pocketcluster.db")
	if _, err := os.Stat(dbPath); err != nil {
		return checkResult{"Data directory", "warn", fmt.Sprintf("%s exists but no database found", dataDir)}
	}
	return checkResult{"Data directory", "ok", dataDir}
}

func checkPort(port int) checkResult {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return checkResult{"Port", "ok", fmt.Sprintf("port %d is in use (agent likely running)", port)}
	}
	ln.Close()
	return checkResult{"Port", "ok", fmt.Sprintf("port %d is available", port)}
}

func checkAgentRunning(port int) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/health", port), nil)
	if err != nil {
		return checkResult{"Agent running", "fail", err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return checkResult{"Agent running", "fail", "agent is not responding"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return checkResult{"Agent running", "fail", fmt.Sprintf("status %d", resp.StatusCode)}
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return checkResult{"Agent running", "fail", err.Error()}
	}
	if !envelope.OK {
		return checkResult{"Agent running", "fail", "unexpected response"}
	}
	return checkResult{"Agent running", "ok", fmt.Sprintf("node %s is %s", envelope.Data.NodeID, envelope.Data.Status)}
}

func checkMDNS() checkResult {
	// mDNS operates at the network discovery layer and cannot be fully
	// verified from a single node. We can only confirm the agent reports
	// discovery as active, but this does not prove mDNS is actually
	// functioning on the network.
	return checkResult{"mDNS discovery", "warn", "cannot verify from CLI; check agent logs for mDNS registration"}
}

func checkWebDAV(port int) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, fmt.Sprintf("http://127.0.0.1:%d/dav/", port), nil)
	if err != nil {
		return checkResult{"WebDAV", "fail", err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return checkResult{"WebDAV", "fail", "WebDAV endpoint not responding"}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusMethodNotAllowed {
		return checkResult{"WebDAV", "ok", "endpoint reachable"}
	}
	return checkResult{"WebDAV", "warn", fmt.Sprintf("unexpected status %d", resp.StatusCode)}
}

func checkNodeConnectivity(port int) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/nodes", port), nil)
	if err != nil {
		return checkResult{"Node connectivity", "fail", err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return checkResult{"Node connectivity", "fail", "cannot reach agent"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return checkResult{"Node connectivity", "fail", fmt.Sprintf("status %d", resp.StatusCode)}
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data []struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return checkResult{"Node connectivity", "fail", err.Error()}
	}
	total := len(envelope.Data)
	online := 0
	for _, n := range envelope.Data {
		if n.Status == "online" {
			online++
		}
	}
	return checkResult{"Node connectivity", "ok", fmt.Sprintf("%d nodes known, %d online", total, online)}
}

func checkStorageWritable(dataDir string) checkResult {
	testFile := filepath.Join(dataDir, ".doctor-write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return checkResult{"Storage writable", "fail", fmt.Sprintf("cannot write to %s: %v", dataDir, err)}
	}
	os.Remove(testFile)
	return checkResult{"Storage writable", "ok", dataDir}
}
