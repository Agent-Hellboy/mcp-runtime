package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGroupVersion(t *testing.T) {
	t.Run("has correct group", func(t *testing.T) {
		expected := "mcpruntime.org"
		if GroupVersion.Group != expected {
			t.Errorf("GroupVersion.Group = %q, want %q", GroupVersion.Group, expected)
		}
	})

	t.Run("has correct version", func(t *testing.T) {
		expected := "v1alpha1"
		if GroupVersion.Version != expected {
			t.Errorf("GroupVersion.Version = %q, want %q", GroupVersion.Version, expected)
		}
	})
}

func TestAddToScheme(t *testing.T) {
	t.Run("registers MCPServer type", func(t *testing.T) {
		scheme := runtime.NewScheme()

		err := AddToScheme(scheme)
		if err != nil {
			t.Fatalf("AddToScheme failed: %v", err)
		}

		// Verify MCPServer is registered
		gvk := schema.GroupVersionKind{
			Group:   "mcpruntime.org",
			Version: "v1alpha1",
			Kind:    "MCPServer",
		}

		obj, err := scheme.New(gvk)
		if err != nil {
			t.Fatalf("failed to create MCPServer from scheme: %v", err)
		}

		if _, ok := obj.(*MCPServer); !ok {
			t.Errorf("expected *MCPServer, got %T", obj)
		}
	})

	t.Run("registers MCPServerList type", func(t *testing.T) {
		scheme := runtime.NewScheme()

		err := AddToScheme(scheme)
		if err != nil {
			t.Fatalf("AddToScheme failed: %v", err)
		}

		// Verify MCPServerList is registered
		gvk := schema.GroupVersionKind{
			Group:   "mcpruntime.org",
			Version: "v1alpha1",
			Kind:    "MCPServerList",
		}

		obj, err := scheme.New(gvk)
		if err != nil {
			t.Fatalf("failed to create MCPServerList from scheme: %v", err)
		}

		if _, ok := obj.(*MCPServerList); !ok {
			t.Errorf("expected *MCPServerList, got %T", obj)
		}
	})

	t.Run("idempotent registration", func(t *testing.T) {
		scheme := runtime.NewScheme()

		// Register twice should not error
		if err := AddToScheme(scheme); err != nil {
			t.Fatalf("first AddToScheme failed: %v", err)
		}
		if err := AddToScheme(scheme); err != nil {
			t.Fatalf("second AddToScheme failed: %v", err)
		}
	})
}

func TestSchemeBuilder(t *testing.T) {
	t.Run("SchemeBuilder is not nil", func(t *testing.T) {
		if SchemeBuilder == nil {
			t.Error("SchemeBuilder should not be nil")
		}
	})

	t.Run("SchemeBuilder has correct GroupVersion", func(t *testing.T) {
		if SchemeBuilder.GroupVersion != GroupVersion {
			t.Errorf("SchemeBuilder.GroupVersion = %v, want %v", SchemeBuilder.GroupVersion, GroupVersion)
		}
	})
}

func TestMCPServerTypeMeta(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = AddToScheme(scheme)

	t.Run("MCPServer has correct GVK", func(t *testing.T) {
		server := &MCPServer{}
		gvks, _, err := scheme.ObjectKinds(server)
		if err != nil {
			t.Fatalf("failed to get object kinds: %v", err)
		}

		if len(gvks) == 0 {
			t.Fatal("expected at least one GVK")
		}

		gvk := gvks[0]
		if gvk.Group != "mcpruntime.org" {
			t.Errorf("GVK.Group = %q, want %q", gvk.Group, "mcpruntime.org")
		}
		if gvk.Version != "v1alpha1" {
			t.Errorf("GVK.Version = %q, want %q", gvk.Version, "v1alpha1")
		}
		if gvk.Kind != "MCPServer" {
			t.Errorf("GVK.Kind = %q, want %q", gvk.Kind, "MCPServer")
		}
	})

	t.Run("MCPServerList has correct GVK", func(t *testing.T) {
		list := &MCPServerList{}
		gvks, _, err := scheme.ObjectKinds(list)
		if err != nil {
			t.Fatalf("failed to get object kinds: %v", err)
		}

		if len(gvks) == 0 {
			t.Fatal("expected at least one GVK")
		}

		gvk := gvks[0]
		if gvk.Group != "mcpruntime.org" {
			t.Errorf("GVK.Group = %q, want %q", gvk.Group, "mcpruntime.org")
		}
		if gvk.Version != "v1alpha1" {
			t.Errorf("GVK.Version = %q, want %q", gvk.Version, "v1alpha1")
		}
		if gvk.Kind != "MCPServerList" {
			t.Errorf("GVK.Kind = %q, want %q", gvk.Kind, "MCPServerList")
		}
	})
}
