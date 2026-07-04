package server

import (
	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

// isIOLMap builds the node-id -> is-IOL lookup the fabric helpers need.
func isIOLMap(doc *lab.Lab) map[int]bool {
	m := make(map[int]bool, len(doc.Nodes))
	for _, n := range doc.Nodes {
		if n.Kind == lab.KindIOL {
			m[n.ID] = true
		}
	}
	return m
}
