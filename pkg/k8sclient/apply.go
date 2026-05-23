package k8sclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
)

const defaultFieldManager = "mcp-runtime"

// ApplyResult describes one object applied through the Kubernetes API.
type ApplyResult struct {
	GroupVersionKind schema.GroupVersionKind
	Namespace        string
	Name             string
	Action           string
}

// String returns kubectl-like output for CLI callers.
func (r ApplyResult) String() string {
	kind := strings.ToLower(r.GroupVersionKind.Kind)
	return fmt.Sprintf("%s/%s %s", kind, r.Name, r.Action)
}

// ApplyManifestYAML applies a multi-document Kubernetes manifest through
// client-go instead of shelling out to kubectl.
func ApplyManifestYAML(ctx context.Context, clients *Clients, manifest []byte, namespace string) ([]ApplyResult, error) {
	if clients == nil {
		return nil, fmt.Errorf("kubernetes clients cannot be nil")
	}
	if clients.Dynamic == nil {
		return nil, fmt.Errorf("dynamic Kubernetes client cannot be nil")
	}
	if clients.Discovery == nil {
		return nil, fmt.Errorf("kubernetes discovery client cannot be nil")
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(clients.Discovery))
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(manifest), 4096)
	results := []ApplyResult{}

	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return results, fmt.Errorf("decode Kubernetes manifest: %w", err)
		}
		if isEmptyObject(obj) {
			continue
		}
		result, err := applyObject(ctx, clients, mapper, obj, namespace)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func applyObject(ctx context.Context, clients *Clients, mapper meta.RESTMapper, obj *unstructured.Unstructured, namespace string) (ApplyResult, error) {
	gvk := obj.GroupVersionKind()
	if gvk.Empty() {
		return ApplyResult{}, fmt.Errorf("manifest object is missing apiVersion or kind")
	}
	name := strings.TrimSpace(obj.GetName())
	if name == "" {
		return ApplyResult{}, fmt.Errorf("%s is missing metadata.name", gvk.String())
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("map %s to Kubernetes resource: %w", gvk.String(), err)
	}

	resourceClient := clients.Dynamic.Resource(mapping.Resource)
	var objectClient dynamic.ResourceInterface = resourceClient
	objectNamespace := ""
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		objectNamespace = firstNonEmpty(strings.TrimSpace(namespace), strings.TrimSpace(obj.GetNamespace()), strings.TrimSpace(clients.Namespace), metav1.NamespaceDefault)
		obj.SetNamespace(objectNamespace)
		objectClient = resourceClient.Namespace(objectNamespace)
	}

	result := ApplyResult{GroupVersionKind: gvk, Namespace: objectNamespace, Name: name}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := objectClient.Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			if _, createErr := objectClient.Create(ctx, obj.DeepCopy(), metav1.CreateOptions{FieldManager: defaultFieldManager}); createErr != nil {
				return fmt.Errorf("create %s %s/%s: %w", gvk.String(), objectNamespace, name, createErr)
			}
			result.Action = "created"
			return nil
		}
		if err != nil {
			return fmt.Errorf("get %s %s/%s: %w", gvk.String(), objectNamespace, name, err)
		}

		merged := current.DeepCopy()
		mergeObject(merged.Object, obj.Object)
		unstructured.RemoveNestedField(merged.Object, "metadata", "managedFields")
		unstructured.RemoveNestedField(merged.Object, "status")
		if _, updateErr := objectClient.Update(ctx, merged, metav1.UpdateOptions{FieldManager: defaultFieldManager}); updateErr != nil {
			return updateErr
		}
		result.Action = "configured"
		return nil
	}); err != nil {
		return ApplyResult{}, fmt.Errorf("apply %s %s/%s: %w", gvk.String(), objectNamespace, name, err)
	}
	return result, nil
}

func isEmptyObject(obj *unstructured.Unstructured) bool {
	return len(obj.Object) == 0 || (obj.GetAPIVersion() == "" && obj.GetKind() == "" && obj.GetName() == "")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mergeObject(dst, src map[string]any) {
	for key, srcValue := range src {
		srcMap, srcMapOK := srcValue.(map[string]any)
		dstMap, dstMapOK := dst[key].(map[string]any)
		if srcMapOK && dstMapOK {
			mergeObject(dstMap, srcMap)
			continue
		}
		dst[key] = sanitizeObjectValue(srcValue)
	}
}

func sanitizeObjectValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, nested := range typed {
			copied[key] = sanitizeObjectValue(nested)
		}
		return copied
	case []any:
		copied := make([]any, len(typed))
		for i, nested := range typed {
			copied[i] = sanitizeObjectValue(nested)
		}
		return copied
	case resource.Quantity:
		return typed.DeepCopy()
	default:
		return typed
	}
}
