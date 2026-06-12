package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ComputeRevision returns a deterministic SHA-256 digest of the canonical
// rendered policy content. The Revision and GeneratedAt fields are excluded
// from the digest so that the revision depends only on policy content and
// neither the previously stamped revision nor the (informational) generation
// timestamp can change it. SchemaVersion is included: a schema bump is a
// meaningful contract change and must produce a new revision.
//
// Determinism relies on encoding/json marshaling struct fields in declaration
// order and map keys in sorted order, which the standard library guarantees.
func ComputeRevision(doc *Document) (string, error) {
	if doc == nil {
		return "", errors.New("policy: cannot compute revision of nil document")
	}
	canonical := *doc
	canonical.Revision = ""
	canonical.GeneratedAt = ""
	data, err := json.Marshal(&canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Stamp sets the document-level metadata on doc: the current SchemaVersion, the
// supplied (informational) generatedAt timestamp, and a freshly computed
// deterministic Revision. generatedAt may be empty; it never affects Revision.
func Stamp(doc *Document, generatedAt string) error {
	if doc == nil {
		return errors.New("policy: cannot stamp nil document")
	}
	doc.SchemaVersion = SchemaVersion
	doc.GeneratedAt = generatedAt
	revision, err := ComputeRevision(doc)
	if err != nil {
		return err
	}
	doc.Revision = revision
	return nil
}
