package platform

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"mcp-runtime/pkg/k8sclient"
)

var newKubernetesClients = k8sclient.New

func platformKubernetesClients() (*k8sclient.Clients, error) {
	clients, err := newKubernetesClients()
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func applyManifestYAML(manifest string, namespace string, stdout io.Writer) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	results, err := k8sclient.ApplyManifestYAML(context.Background(), clients, []byte(manifest), namespace)
	if err != nil {
		return err
	}
	writeApplyResults(stdout, results)
	return nil
}

func applyManifestFile(path string, namespace string, stdout io.Writer) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	results, err := k8sclient.ApplyManifestFile(context.Background(), clients, path, namespace)
	if err != nil {
		return err
	}
	writeApplyResults(stdout, results)
	return nil
}

func applyManifestDir(path string, namespace string, stdout io.Writer) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	results, err := k8sclient.ApplyManifestDir(context.Background(), clients, path, namespace)
	if err != nil {
		return err
	}
	writeApplyResults(stdout, results)
	return nil
}

func ensureNamespaceWithLabels(name string, labels map[string]string) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	return k8sclient.EnsureNamespace(context.Background(), clients, name, labels)
}

func writeApplyResults(stdout io.Writer, results []k8sclient.ApplyResult) {
	if stdout == nil {
		stdout = os.Stdout
	}
	for _, result := range results {
		if strings.TrimSpace(result.Action) == "" {
			continue
		}
		fmt.Fprintln(stdout, result.String())
	}
}
