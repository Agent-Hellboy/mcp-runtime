package policy

import "testing"

func validStampedDocument() *Document {
	return &Document{
		Server: Server{Name: "demo", Namespace: "mcp-servers"},
		Policy: &Config{Mode: "allow-list", DefaultDecision: "deny", PolicyVersion: "v1"},
		Tools:  []Tool{{Name: "echo", RequiredTrust: "low", SideEffect: "read"}},
	}
}

func TestComputeRevisionDeterministic(t *testing.T) {
	doc := validStampedDocument()
	first, err := ComputeRevision(doc)
	if err != nil {
		t.Fatalf("ComputeRevision() error = %v", err)
	}
	second, err := ComputeRevision(doc)
	if err != nil {
		t.Fatalf("ComputeRevision() error = %v", err)
	}
	if first != second {
		t.Fatalf("revision not deterministic: %q != %q", first, second)
	}
	if len(first) <= len("sha256:") || first[:len("sha256:")] != "sha256:" {
		t.Fatalf("revision = %q, want sha256: prefix", first)
	}
}

func TestComputeRevisionIgnoresGeneratedAtAndRevision(t *testing.T) {
	base := validStampedDocument()
	baseRev, err := ComputeRevision(base)
	if err != nil {
		t.Fatalf("ComputeRevision() error = %v", err)
	}

	withMeta := validStampedDocument()
	withMeta.GeneratedAt = "2026-01-01T00:00:00Z"
	withMeta.Revision = "sha256:stale"
	metaRev, err := ComputeRevision(withMeta)
	if err != nil {
		t.Fatalf("ComputeRevision() error = %v", err)
	}
	if baseRev != metaRev {
		t.Fatalf("GeneratedAt/Revision affected digest: %q != %q", baseRev, metaRev)
	}
}

func TestComputeRevisionChangesWithContent(t *testing.T) {
	base := validStampedDocument()
	baseRev, _ := ComputeRevision(base)

	changed := validStampedDocument()
	changed.Tools[0].RequiredTrust = "high"
	changedRev, _ := ComputeRevision(changed)
	if baseRev == changedRev {
		t.Fatal("content change did not change revision")
	}
}

func TestStampSetsMetadata(t *testing.T) {
	doc := validStampedDocument()
	if err := Stamp(doc, "2026-06-10T12:00:00Z"); err != nil {
		t.Fatalf("Stamp() error = %v", err)
	}
	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", doc.SchemaVersion, SchemaVersion)
	}
	if doc.GeneratedAt != "2026-06-10T12:00:00Z" {
		t.Fatalf("GeneratedAt = %q, want stamped value", doc.GeneratedAt)
	}
	want, _ := ComputeRevision(doc)
	if doc.Revision != want {
		t.Fatalf("Revision = %q, want %q", doc.Revision, want)
	}
	if err := Validate(doc); err != nil {
		t.Fatalf("stamped document failed validation: %v", err)
	}
}

func TestComputeRevisionNilDocument(t *testing.T) {
	if _, err := ComputeRevision(nil); err == nil {
		t.Fatal("ComputeRevision(nil) error = nil, want error")
	}
}
