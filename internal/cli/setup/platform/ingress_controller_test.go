package platform

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngressControllerIdentityFromK3sTraefik(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "traefik", Namespace: "kube-system"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app.kubernetes.io/name":     "traefik",
				"app.kubernetes.io/instance": "traefik-kube-system",
			}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{ServiceAccountName: "traefik"}},
		},
	}

	env := map[string]string{}
	for _, ev := range ingressControllerOperatorEnv(ingressControllerIdentityFromDeployment(deployment)) {
		env[ev.Name] = ev.Value
	}
	for name, want := range map[string]string{
		"MCP_INGRESS_CONTROLLER_NAMESPACE":       "kube-system",
		"MCP_INGRESS_CONTROLLER_SERVICE_ACCOUNT": "traefik",
		"MCP_INGRESS_CONTROLLER_POD_LABELS":      "app.kubernetes.io/instance=traefik-kube-system,app.kubernetes.io/name=traefik",
	} {
		if env[name] != want {
			t.Errorf("%s = %q, want %q", name, env[name], want)
		}
	}
}

func TestIngressControllerIdentityDefaultsServiceAccount(t *testing.T) {
	identity := ingressControllerIdentityFromDeployment(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "traefik", Namespace: "traefik"},
	})
	if identity.ServiceAccount != "default" {
		t.Fatalf("ServiceAccount = %q, want default", identity.ServiceAccount)
	}
	if identity.PodLabels != nil {
		t.Fatalf("PodLabels = %v, want nil without a selector", identity.PodLabels)
	}
}

func TestIngressControllerOperatorEnvEmptyWhenUndetected(t *testing.T) {
	if got := ingressControllerOperatorEnv(ingressControllerIdentity{}); len(got) != 0 {
		t.Fatalf("env = %v, want none so the operator keeps its defaults", got)
	}
}
