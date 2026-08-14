package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func writeNDJSON(conn net.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAll(conn, data)
}

func serveNDJSONOnce(t *testing.T, fn func(net.Conn, map[string]any) error) *ndjsonClient {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go func() {
		defer serverConn.Close()
		line, err := bufio.NewReader(serverConn).ReadBytes('\n')
		if err != nil {
			t.Errorf("server read request: %v", err)
			return
		}
		var request map[string]any
		if err := json.Unmarshal(line, &request); err != nil {
			t.Errorf("server decode request: %v", err)
			return
		}
		if err := fn(serverConn, request); err != nil {
			t.Errorf("server exchange: %v", err)
		}
	}()
	return newNDJSONClient(clientConn, 2*time.Second)
}

func responseFor(request map[string]any, ok bool, result any) map[string]any {
	return map[string]any{"id": request["id"], "ok": ok, "result": result}
}

func TestNDJSONCorrelatesStringIDPastEventsAndMismatches(t *testing.T) {
	client := serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
		if _, ok := request["id"].(string); !ok {
			return fmt.Errorf("wire request id has type %T, want string", request["id"])
		}
		if err := writeNDJSON(conn, map[string]any{"event": "host.stats", "data": map[string]any{"cpu": 1}}); err != nil {
			return err
		}
		if err := writeNDJSON(conn, map[string]any{"id": "wrong", "ok": true, "result": map[string]any{"id": "wrong-result"}}); err != nil {
			return err
		}
		return writeNDJSON(conn, responseFor(request, true, map[string]string{"id": "inner-result", "value": "ok"}))
	})
	defer client.Close()
	result, err := client.request(t.Context(), "hello", map[string]string{"client": "test"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["id"] != "inner-result" {
		t.Fatalf("result id = %q, want inner result id", decoded["id"])
	}
}

func TestNDJSONErrorEnvelope(t *testing.T) {
	client := serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
		return writeNDJSON(conn, map[string]any{"id": request["id"], "ok": false, "error": map[string]string{"code": "bad_request", "message": "nope"}})
	})
	defer client.Close()
	if _, err := client.request(t.Context(), "hello", nil); err == nil || !strings.Contains(err.Error(), "bad_request") {
		t.Fatalf("error = %v", err)
	}
}

func TestNDJSONOversizedFrameAndTypedProtocolContracts(t *testing.T) {
	large := strings.Repeat("x", 70*1024)
	client := serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
		return writeNDJSON(conn, responseFor(request, true, map[string]string{"blob": large}))
	})
	result, err := client.request(t.Context(), "large", map[string]any{})
	client.Close()
	if err != nil || len(result) < 70*1024 {
		t.Fatalf("large result length/error = %d/%v", len(result), err)
	}

	t.Run("YAML save and list", func(t *testing.T) {
		client := serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
			args := request["args"].(map[string]any)
			if _, ok := args["lab"].(string); !ok {
				return fmt.Errorf("saveDoc lab type = %T, want string", args["lab"])
			}
			return writeNDJSON(conn, responseFor(request, true, map[string]string{"id": "yaml-id"}))
		})
		id, err := client.labSaveDoc(t.Context(), "version: 1\nid: yaml-id\n")
		client.Close()
		if err != nil || id != "yaml-id" {
			t.Fatalf("save result = %q/%v", id, err)
		}

		client = serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
			return writeNDJSON(conn, responseFor(request, true, map[string][]string{"labs": {"version: 1\nid: a\n", "version: 1\nid: b\n"}}))
		})
		labs, err := client.labListDocs(t.Context())
		client.Close()
		if err != nil || len(labs) != 2 || !strings.Contains(labs[0], "id: a") {
			t.Fatalf("list result = %#v/%v", labs, err)
		}
	})

	t.Run("structured load and semantic failures", func(t *testing.T) {
		client := serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
			args := request["args"].(map[string]any)
			if _, ok := args["lab"].(map[string]any); !ok {
				return fmt.Errorf("load lab type = %T, want object", args["lab"])
			}
			return writeNDJSON(conn, responseFor(request, true, map[string]any{"labId": "l", "warnings": []string{"warning"}}))
		})
		if _, err := client.labLoad(t.Context(), json.RawMessage(`{"version":1,"id":"l"}`)); err == nil || !strings.Contains(err.Error(), "warnings") {
			t.Fatalf("lab.load warning error = %v", err)
		}
		client.Close()

		client = serveNDJSONOnce(t, func(conn net.Conn, request map[string]any) error {
			return writeNDJSON(conn, responseFor(request, true, map[string]any{"failed": []map[string]any{{"node": 1}}}))
		})
		if _, err := client.labStart(t.Context(), map[string]string{"labId": "l"}); err == nil || !strings.Contains(err.Error(), "failed") {
			t.Fatalf("lab.start failure error = %v", err)
		}
		client.Close()

		clientConn, serverConn := net.Pipe()
		client = newNDJSONClient(clientConn, time.Second)
		if _, err := client.labLoad(t.Context(), json.RawMessage(`"yaml text"`)); err == nil || !strings.Contains(err.Error(), "structured JSON object") {
			t.Fatalf("YAML lab.load input error = %v", err)
		}
		client.Close()
		serverConn.Close()
	})
}

func TestNDJSONContextDeadline(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	go func() {
		_, _ = bufio.NewReader(serverConn).ReadBytes('\n')
		// Intentionally do not reply.
	}()
	client := newNDJSONClient(clientConn, 50*time.Millisecond)
	_, err := client.request(context.Background(), "hello", nil)
	if err == nil {
		t.Fatal("request without reply unexpectedly succeeded")
	}
}
