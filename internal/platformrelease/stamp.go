package platformrelease

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// MetadataPatch returns a merge patch that sets the platform labels and,
// for MCP Runtime-built components, the platform-version annotation on a
// Deployment's own metadata. It never touches the pod template.
func MetadataPatch(c Component, version string) map[string]any {
	meta := map[string]any{
		"labels": map[string]string{
			LabelPartOf:    LabelPartOfValue,
			LabelComponent: c.Name,
		},
	}
	if c.Built && version != "" {
		meta["annotations"] = map[string]string{AnnotationVersion: version}
	}
	return meta
}

// StampInstalledVersion labels and annotates every installed MCP Runtime
// Deployment with the platform version setup just deployed. Missing
// Deployments are skipped. Third-party components (cert-manager) are never
// stamped. Metadata-only patches do not trigger rollouts.
func StampInstalledVersion(ctx context.Context, cs kubernetes.Interface, version string) error {
	seen := map[string]bool{}
	for _, c := range catalog {
		if !c.HasWorkload() || c.OptIn == OptInCertManager || c.EnvVar != "" {
			continue
		}
		key := c.Namespace + "/" + c.Deployment
		if seen[key] {
			continue
		}
		seen[key] = true
		patch, err := json.Marshal(map[string]any{"metadata": MetadataPatch(c, version)})
		if err != nil {
			return err
		}
		_, err = cs.AppsV1().Deployments(c.Namespace).Patch(ctx, c.Deployment, types.MergePatchType, patch, metav1.PatchOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stamp %s version metadata: %w", key, err)
		}
	}
	return nil
}
