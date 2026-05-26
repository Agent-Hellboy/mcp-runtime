package platform

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/sentinel"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ensureSessionLocalDeploymentReplicasClientGo(logger *zap.Logger) error {
	clients, err := platformKubernetesClients()
	if err != nil {
		return err
	}
	deployments := clients.Clientset.AppsV1().Deployments(core.DefaultAnalyticsNamespace)
	for _, name := range sentinel.SessionLocalDeploymentNames {
		current, err := deployments.Get(context.Background(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read deployment %s/%s replica count: %w", core.DefaultAnalyticsNamespace, name, err)
		}
		desired := sentinel.SessionLocalMaxReplicas
		if current.Spec.Replicas != nil && *current.Spec.Replicas == desired {
			continue
		}
		if logger != nil {
			logger.Warn("scaling session-local deployment back to single replica",
				zap.String("deployment", name),
				zap.Int32("requested", replicaCount(current.Spec.Replicas)),
				zap.Int32("enforced", desired),
			)
		}
		copy := current.DeepCopy()
		copy.Spec.Replicas = &desired
		if _, err := deployments.Update(context.Background(), copy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("scale deployment %s/%s to %d replicas: %w", core.DefaultAnalyticsNamespace, name, desired, err)
		}
	}
	return nil
}

func ensureSessionLocalDeploymentReplicas(kubectl core.KubectlRunner, logger *zap.Logger) error {
	for _, name := range sentinel.SessionLocalDeploymentNames {
		out, err := kubectlText(kubectl, []string{
			"get", "deployment", name,
			"-n", core.DefaultAnalyticsNamespace,
			"-o", "jsonpath={.spec.replicas}",
		})
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		current, err := parseReplicaCount(out)
		if err != nil {
			return fmt.Errorf("parse deployment %s/%s replica count: %w", core.DefaultAnalyticsNamespace, name, err)
		}
		if current == sentinel.SessionLocalMaxReplicas {
			continue
		}
		if logger != nil {
			logger.Warn("scaling session-local deployment back to single replica",
				zap.String("deployment", name),
				zap.Int32("requested", current),
				zap.Int32("enforced", sentinel.SessionLocalMaxReplicas),
			)
		}
		if err := kubectl.RunWithOutput([]string{
			"scale", "deployment/" + name,
			"-n", core.DefaultAnalyticsNamespace,
			fmt.Sprintf("--replicas=%d", sentinel.SessionLocalMaxReplicas),
		}, nil, nil); err != nil {
			return fmt.Errorf("scale deployment %s/%s to %d replicas: %w", core.DefaultAnalyticsNamespace, name, sentinel.SessionLocalMaxReplicas, err)
		}
	}
	return nil
}

func replicaCount(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

func parseReplicaCount(raw string) (int32, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("negative replica count %d", value)
	}
	return int32(value), nil
}
