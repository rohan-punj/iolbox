package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// The durable lab-document store persists whole lab documents as
// <LabsDir>/<id>.json, one file per lab, stored byte-for-byte as received (no
// re-marshalling through the lab struct, so unknown fields survive). It is
// deliberately separate from the runtime lab.load/lab.start lifecycle: saving a
// document here does not load or start it, and loading a runtime lab does not
// touch the store.

// labIDPattern is the set of id tokens accepted as a safe on-disk filename
// base: one or more ASCII letters, digits, underscores, or hyphens. Anything
// else (path separators, dots, empty) is rejected so a lab id can never escape
// LabsDir or collide with a traversal sequence.
var labIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// labDocPath returns the on-disk path for a stored lab id after validating the
// id is a safe filename token. An invalid id yields a schema_invalid error.
func (s *Server) labDocPath(id string) (string, error) {
	if !labIDPattern.MatchString(id) {
		return "", protocol.Errorf(protocol.CodeSchemaInvalid, "lab id %q is not a valid store token ([A-Za-z0-9_-]+)", id)
	}
	return filepath.Join(s.cfg.LabsDir, id+".json"), nil
}

// handleLabSaveDoc persists the raw lab document to <LabsDir>/<id>.json,
// overwriting any existing copy. The id comes from the document's own "id"
// field and must be a safe filename token. The bytes are stored exactly as
// received (unknown fields preserved). Creates LabsDir on first save.
func (s *Server) handleLabSaveDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabSaveDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Lab) == 0 {
		return nil, protocol.NewError(protocol.CodeSchemaInvalid, "lab document is required")
	}
	// Pull just the id out of the raw doc without disturbing the rest.
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args.Lab, &head); err != nil {
		return nil, protocol.Errorf(protocol.CodeSchemaInvalid, "lab document is not valid JSON: %v", err)
	}
	if head.ID == "" {
		return nil, protocol.NewError(protocol.CodeSchemaInvalid, "lab document has no id")
	}
	path, err := s.labDocPath(head.ID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.cfg.LabsDir, 0o755); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "labs dir %s: %v", s.cfg.LabsDir, err)
	}
	if err := os.WriteFile(path, args.Lab, 0o644); err != nil {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "write lab %s: %v", head.ID, err)
	}
	return protocol.LabSaveDocResult{ID: head.ID}, nil
}

// handleLabListDocs returns every stored lab document, parsed back from disk. A
// missing store dir yields an empty list; individual files that cannot be read
// or parsed are skipped with a log line so one bad file never fails the list.
func (s *Server) handleLabListDocs(_ json.RawMessage) (any, error) {
	out := protocol.LabListDocsResult{Labs: []json.RawMessage{}}
	entries, err := os.ReadDir(s.cfg.LabsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // no store yet = no docs
		}
		return nil, protocol.Errorf(protocol.CodeBadRequest, "read labs dir %s: %v", s.cfg.LabsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.cfg.LabsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("labstore: skipping unreadable lab %s: %v", e.Name(), err)
			continue
		}
		if !json.Valid(data) {
			log.Printf("labstore: skipping malformed lab %s (invalid JSON)", e.Name())
			continue
		}
		out.Labs = append(out.Labs, json.RawMessage(data))
	}
	return out, nil
}

// handleLabGetDoc returns the stored lab document for labId, or a not_found
// error if no such document exists.
func (s *Server) handleLabGetDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabGetDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	path, err := s.labDocPath(args.LabID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, protocol.Errorf(protocol.CodeNotFound, "lab %q not found", args.LabID)
		}
		return nil, protocol.Errorf(protocol.CodeBadRequest, "read lab %s: %v", args.LabID, err)
	}
	return protocol.LabGetDocResult{Lab: json.RawMessage(data)}, nil
}

// handleLabDeleteDoc removes the stored lab document for labId. A missing file
// is not an error (delete is idempotent).
func (s *Server) handleLabDeleteDoc(raw json.RawMessage) (any, error) {
	var args protocol.LabGetDocArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	path, err := s.labDocPath(args.LabID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, protocol.Errorf(protocol.CodeBadRequest, "delete lab %s: %v", args.LabID, err)
	}
	return struct{}{}, nil
}
