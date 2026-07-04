package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// The durable lab-document store persists whole lab documents as
// <LabsDir>/<id>.yml (iolbox's native format is YAML), one file per lab, stored
// byte-for-byte as received. The supervisor treats the document as opaque text:
// it only extracts the id (for the filename) and never re-marshals it, so unknown
// fields and formatting survive. It is deliberately separate from the runtime
// lab.load/lab.start lifecycle. Legacy <id>.json files (labs saved before the
// YAML switch) are still read for back-compat.

// labIDPattern is the set of id tokens accepted as a safe on-disk filename
// base: one or more ASCII letters, digits, underscores, or hyphens.
var labIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// yamlIDLine matches a top-level `id: <value>` line (no indentation) in a YAML
// document, capturing the (optionally quoted) value.
var yamlIDLine = regexp.MustCompile(`(?m)^id:\s*["']?([A-Za-z0-9_-]+)["']?\s*$`)

// labDocID extracts the lab id from a document's text, accepting either YAML
// (the native format) or JSON (legacy). Returns ("", false) if no usable id.
func labDocID(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "{") {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(t), &head) == nil && head.ID != "" {
			return head.ID, true
		}
		return "", false
	}
	if m := yamlIDLine.FindStringSubmatch(text); m != nil {
		return m[1], true
	}
	return "", false
}

// labDocPath returns the on-disk .yml path for a stored lab id after validating
// the id is a safe filename token.
func (s *Server) labDocPath(id string) (string, error) {
	if !labIDPattern.MatchString(id) {
		return "", protocol.Errorf(protocol.CodeSchemaInvalid, "lab id %q is not a valid store token ([A-Za-z0-9_-]+)", id)
	}
	return filepath.Join(s.cfg.LabsDir, id+".yml"), nil
}

// handleLabSaveDoc persists the raw lab document text to <LabsDir>/<id>.yml,
// overwriting any existing copy (and removing a stale legacy <id>.json). The id
// comes from the document's own id field and must be a safe filename token. The
// text is stored exactly as received.
func (s *Server) handleLabSaveDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabSaveDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Lab) == "" {
		return nil, protocol.NewError(protocol.CodeSchemaInvalid, "lab document is required")
	}
	id, ok := labDocID(args.Lab)
	if !ok {
		return nil, protocol.NewError(protocol.CodeSchemaInvalid, "lab document has no id")
	}
	path, err := s.labDocPath(id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.cfg.LabsDir, 0o755); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "labs dir %s: %v", s.cfg.LabsDir, err)
	}
	if err := os.WriteFile(path, []byte(args.Lab), 0o644); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "write lab %s: %v", id, err)
	}
	// Drop a stale legacy JSON copy so the lab isn't listed twice after a re-save.
	_ = os.Remove(filepath.Join(s.cfg.LabsDir, id+".json"))
	return protocol.LabSaveDocResult{ID: id}, nil
}

// handleLabListDocs returns every stored lab document's raw text. Both .yml
// (native) and legacy .json files are returned; a missing store dir yields an
// empty list; unreadable files are skipped with a log line.
func (s *Server) handleLabListDocs(_ json.RawMessage) (any, error) {
	out := protocol.LabListDocsResult{Labs: []string{}}
	entries, err := os.ReadDir(s.cfg.LabsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // no store yet = no docs
		}
		return nil, protocol.Errorf(protocol.CodeBadRequest, "read labs dir %s: %v", s.cfg.LabsDir, err)
	}
	// If both <id>.yml and <id>.json exist (a lab saved after the switch that had
	// a legacy copy), prefer the .yml and skip the .json so it isn't listed twice.
	seenYML := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) == ".yml" {
			if !e.IsDir() {
				seenYML[strings.TrimSuffix(e.Name(), ".yml")] = true
			}
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yml" && ext != ".json" {
			continue
		}
		if ext == ".json" && seenYML[strings.TrimSuffix(e.Name(), ".json")] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.LabsDir, e.Name()))
		if err != nil {
			log.Printf("labstore: skipping unreadable lab %s: %v", e.Name(), err)
			continue
		}
		out.Labs = append(out.Labs, string(data))
	}
	return out, nil
}

// handleLabGetDoc returns the stored lab document text for labId (preferring
// .yml, falling back to a legacy .json), or a not_found error.
func (s *Server) handleLabGetDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabGetDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if !labIDPattern.MatchString(args.LabID) {
		return nil, protocol.Errorf(protocol.CodeSchemaInvalid, "lab id %q is not a valid store token", args.LabID)
	}
	for _, ext := range []string{".yml", ".json"} {
		data, err := os.ReadFile(filepath.Join(s.cfg.LabsDir, args.LabID+ext))
		if err == nil {
			return protocol.LabGetDocResult{Lab: string(data)}, nil
		}
		if !os.IsNotExist(err) {
			return nil, protocol.Errorf(protocol.CodeBadRequest, "read lab %s: %v", args.LabID, err)
		}
	}
	return nil, protocol.Errorf(protocol.CodeNotFound, "lab %q not found", args.LabID)
}

// handleLabDeleteDoc removes the stored lab document for labId (both .yml and a
// legacy .json). A missing file is not an error (delete is idempotent).
func (s *Server) handleLabDeleteDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabGetDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if !labIDPattern.MatchString(args.LabID) {
		return nil, protocol.Errorf(protocol.CodeSchemaInvalid, "lab id %q is not a valid store token", args.LabID)
	}
	for _, ext := range []string{".yml", ".json"} {
		if err := os.Remove(filepath.Join(s.cfg.LabsDir, args.LabID+ext)); err != nil && !os.IsNotExist(err) {
			return nil, protocol.Errorf(protocol.CodeBadRequest, "delete lab %s: %v", args.LabID, err)
		}
	}
	return struct{}{}, nil
}
