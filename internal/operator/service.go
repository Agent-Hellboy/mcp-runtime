package operator

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

func (r *MCPServerReconciler) reconcileService(ctx context.Context, mcpServer *mcpv1alpha1.MCPServer) error {
	logger := log.FromContext(ctx)
	targetPort := mcpServer.Spec.Port
	if gatewayEnabled(mcpServer) {
		targetPort = mcpServer.Spec.Gateway.Port
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, service, func() error {
		labels := map[string]string{
			LabelApp:       mcpServer.Name,
			LabelManagedBy: LabelManagedByValue,
		}
		service.Labels = labels
		service.Annotations = nil
		if gatewayEnabled(mcpServer) {
			service.Annotations = map[string]string{
				"prometheus.io/path":   "/metrics",
				"prometheus.io/port":   strconv.Itoa(int(mcpServer.Spec.Gateway.Port)),
				"prometheus.io/scrape": "true",
			}
		}

		service.Spec = corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       mcpServer.Spec.ServicePort,
					TargetPort: intstr.FromInt32(targetPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		}

		if err := ctrl.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		logger.Info("Service reconciled", "operation", op, "name", service.Name)
	}

	return nil
}
