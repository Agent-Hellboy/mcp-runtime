package platform

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/setup/assetpath"
	"mcp-runtime/pkg/manifest"
)

const (
	operatorWebhookServiceName        = "mcp-runtime-operator-webhook-service"
	operatorWebhookSecretName         = "mcp-runtime-operator-webhook-server-cert" // #nosec G101 -- Kubernetes Secret name.
	operatorWebhookVolumeName         = "webhook-server-cert"
	operatorWebhookCertDir            = "/tmp/k8s-webhook-server/serving-certs"
	operatorWebhookCertHashAnnotation = "mcp-runtime.io/webhook-cert-sha256"
)

func generateOperatorWebhookCertificate(now time.Time) ([]byte, []byte, []byte, error) {
	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate webhook CA private key: %w", err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate webhook private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	caSerialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate webhook CA certificate serial: %w", err)
	}
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate webhook certificate serial: %w", err)
	}

	serviceDNS := operatorWebhookServiceName + "." + core.NamespaceMCPRuntime + ".svc"
	caTemplate := x509.Certificate{
		SerialNumber: caSerialNumber,
		Subject: pkix.Name{
			CommonName: operatorWebhookServiceName + "-ca",
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: serviceDNS,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames: []string{
			operatorWebhookServiceName,
			operatorWebhookServiceName + "." + core.NamespaceMCPRuntime,
			serviceDNS,
			serviceDNS + ".cluster.local",
		},
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPrivateKey.PublicKey, caPrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create webhook CA certificate: %w", err)
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, &caTemplate, &privateKey.PublicKey, caPrivateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create webhook certificate: %w", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return caCertPEM, certPEM, keyPEM, nil
}

func operatorWebhookTLSSecretManifest(certPEM, keyPEM []byte) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: kubernetes.io/tls
data:
  tls.crt: %s
  tls.key: %s
`, operatorWebhookSecretName, core.NamespaceMCPRuntime, base64.StdEncoding.EncodeToString(certPEM), base64.StdEncoding.EncodeToString(keyPEM))
}

func ensureOperatorWebhookTLSSecretClientGo() ([]byte, error) {
	caCertPEM, certPEM, keyPEM, err := generateOperatorWebhookCertificate(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := applyManifestYAML(operatorWebhookTLSSecretManifest(certPEM, keyPEM), "", os.Stdout); err != nil {
		return nil, err
	}
	return caCertPEM, nil
}

func ensureOperatorWebhookTLSSecret(kubectl core.KubectlRunner) ([]byte, error) {
	caCertPEM, certPEM, keyPEM, err := generateOperatorWebhookCertificate(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, operatorWebhookTLSSecretManifest(certPEM, keyPEM)); err != nil {
		return nil, err
	}
	return caCertPEM, nil
}

func configureOperatorWebhookDeployment(mutator *manifest.Mutator, caBundlePEM []byte) error {
	if err := mutator.MergeDeploymentEnv(core.OperatorDeploymentName, core.OperatorManagerContainerName, map[string]string{
		"MCP_ENABLE_WEBHOOKS": "true",
	}); err != nil {
		return fmt.Errorf("enable operator webhooks: %w", err)
	}
	webhookCAHash := sha256.Sum256(caBundlePEM)
	if err := mutator.MergeDeploymentTemplateAnnotations(core.OperatorDeploymentName, map[string]string{
		operatorWebhookCertHashAnnotation: hex.EncodeToString(webhookCAHash[:]),
	}); err != nil {
		return fmt.Errorf("annotate operator webhook certificate hash: %w", err)
	}
	if err := mutator.MergeDeploymentVolumes(core.OperatorDeploymentName, []map[string]any{{
		"name": operatorWebhookVolumeName,
		"secret": map[string]any{
			"secretName": operatorWebhookSecretName,
		},
	}}); err != nil {
		return fmt.Errorf("add operator webhook certificate volume: %w", err)
	}
	if err := mutator.MergeDeploymentVolumeMounts(core.OperatorDeploymentName, core.OperatorManagerContainerName, []map[string]any{{
		"name":      operatorWebhookVolumeName,
		"mountPath": operatorWebhookCertDir,
		"readOnly":  true,
	}}); err != nil {
		return fmt.Errorf("add operator webhook certificate mount: %w", err)
	}
	return nil
}

func applyOperatorWebhookManifestsClientGo(caBundlePEM []byte) error {
	servicePath, err := assetpath.ResolveRepoAssetPath("config/webhook/service.yaml")
	if err != nil {
		return err
	}
	if err := applyManifestFile(servicePath, "", os.Stdout); err != nil {
		return err
	}

	webhookYAML, err := readRepoAsset("config/webhook/manifests.yaml")
	if err != nil {
		return fmt.Errorf("read operator webhook manifests: %w", err)
	}
	rendered, err := injectOperatorWebhookCABundle(webhookYAML, caBundlePEM)
	if err != nil {
		return err
	}
	return applyManifestYAML(string(rendered), "", os.Stdout)
}

func applyOperatorWebhookManifests(kubectl core.KubectlRunner, caBundlePEM []byte) error {
	serviceYAML, err := readRepoAsset("config/webhook/service.yaml")
	if err != nil {
		return fmt.Errorf("read operator webhook service manifest: %w", err)
	}
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, string(serviceYAML)); err != nil {
		return err
	}

	webhookYAML, err := readRepoAsset("config/webhook/manifests.yaml")
	if err != nil {
		return fmt.Errorf("read operator webhook manifests: %w", err)
	}
	rendered, err := injectOperatorWebhookCABundle(webhookYAML, caBundlePEM)
	if err != nil {
		return err
	}
	return kube.ApplyManifestContent(kubectl.CommandArgs, string(rendered))
}

func readRepoAsset(path string) ([]byte, error) {
	rootPath, err := assetpath.ResolveRepoRoot()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open repo root: %w", err)
	}
	defer root.Close()
	return root.ReadFile(path)
}

func injectOperatorWebhookCABundle(webhookYAML, caBundlePEM []byte) ([]byte, error) {
	caBundle := base64.StdEncoding.EncodeToString(caBundlePEM)
	decoder := yaml.NewDecoder(bytes.NewReader(webhookYAML))
	var docs []map[string]any
	injected := 0

	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode webhook manifest: %w", err)
		}
		if len(doc) == 0 {
			continue
		}
		qualifyWebhookConfigurationName(doc)

		webhooks, ok := doc["webhooks"].([]any)
		if !ok {
			docs = append(docs, doc)
			continue
		}
		for _, item := range webhooks {
			webhook, ok := item.(map[string]any)
			if !ok {
				continue
			}
			clientConfig, ok := webhook["clientConfig"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("webhook %q has no clientConfig", stringValue(webhook, "name"))
			}
			clientConfig["caBundle"] = caBundle
			injected++
		}
		docs = append(docs, doc)
	}
	if injected == 0 {
		return nil, fmt.Errorf("no webhook clientConfig blocks found")
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	for i, doc := range docs {
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode webhook manifest %d: %w", i, err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close webhook manifest encoder: %w", err)
	}
	return out.Bytes(), nil
}

func qualifyWebhookConfigurationName(doc map[string]any) {
	kind := stringValue(doc, "kind")
	var name string
	switch kind {
	case "MutatingWebhookConfiguration":
		name = "mcp-runtime-mutating-webhook-configuration"
	case "ValidatingWebhookConfiguration":
		name = "mcp-runtime-validating-webhook-configuration"
	default:
		return
	}
	metadata, ok := doc["metadata"].(map[string]any)
	if !ok {
		metadata = map[string]any{}
		doc["metadata"] = metadata
	}
	metadata["name"] = name
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
