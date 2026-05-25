package runtimeapi

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-runtime/pkg/publishscope"
	"mcp-runtime/pkg/registrypush"
)

const (
	registryServiceName    = "registry"
	registryPushTempDirEnv = "MCP_REGISTRY_PUSH_TEMP_DIR"
)

const registryPushMaxBytes = 512 << 20

type registryPushRequest struct {
	Target  string
	Scope   publishscope.Scope
	TarPath string
}

// HandleRuntimeRegistryPush accepts a docker save tar and pushes it to the
// platform registry from inside the cluster.
func (s *RuntimeServer) HandleRuntimeRegistryPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("allow", "POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.k8sClients == nil || s.k8sClients.Clientset == nil || s.k8sClients.Config == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}
	clients := s.k8sClients

	r.Body = http.MaxBytesReader(w, r.Body, registryPushMaxBytes)
	req, err := readRegistryPushRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(req.TarPath)

	namespace, teamSlug, err := registryPushAuthContext(req.Target, req.Scope, p)
	if err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := ValidateDeployImage(req.Target, namespace, teamSlug, p.Role); err != nil {
		writeAPIError(w, http.StatusForbidden, err.Error())
		return
	}
	if p.Role != roleAdmin && namespace != "" && !principalCanPublishNamespace(p, namespace) {
		writeAPIError(w, http.StatusForbidden, "forbidden namespace")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	helperNS := strings.TrimSpace(os.Getenv("MCP_REGISTRY_PUSH_HELPER_NAMESPACE"))
	if helperNS == "" {
		helperNS = "mcp-sentinel"
	}
	if err := registrypush.EnsureHelperNamespace(ctx, clients.Clientset, helperNS); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	transferToken, fetchURL, err := s.registerRegistryPushTransfer(ctx, req.TarPath, 15*time.Minute)
	if err != nil {
		log.Printf("registry push transfer registration failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "failed to prepare image transfer")
		return
	}
	pushSucceeded := false
	defer func() {
		if !pushSucceeded {
			s.revokeRegistryPushTransfer(context.Background(), transferToken)
		}
	}()

	cfg := registryPushConfig(helperNS)
	cfg.TarFetchURL = fetchURL
	cfg.TarFetchToken = transferToken
	rewrittenTarget := registrypush.RewritePushTarget(req.Target, cfg.Hosts)
	if tarInfo, statErr := os.Stat(req.TarPath); statErr == nil {
		log.Printf("registry push: target=%q rewritten=%q scope=%q tar_bytes=%d helper_ns=%q", req.Target, rewrittenTarget, req.Scope, tarInfo.Size(), helperNS)
	} else {
		log.Printf("registry push: target=%q rewritten=%q scope=%q helper_ns=%q", req.Target, rewrittenTarget, req.Scope, helperNS)
	}

	// Upload parsing uses the request context; the in-cluster push should keep
	// running even if the client disconnects after the body is fully received.
	pushCtx, pushCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer pushCancel()

	if err := registrypush.PushDockerArchive(pushCtx, clients.Clientset, clients.Config, req.TarPath, req.Target, cfg); err != nil {
		log.Printf("registry push failed: target=%q rewritten=%q: %v", req.Target, rewrittenTarget, err)
		writeAPIError(w, http.StatusInternalServerError, "registry push failed")
		return
	}
	pushSucceeded = true
	log.Printf("registry push succeeded: target=%q rewritten=%q", req.Target, rewrittenTarget)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"target":  req.Target,
	})
}

func readRegistryPushRequest(r *http.Request) (out registryPushRequest, err error) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return registryPushRequest{}, fmt.Errorf("invalid multipart form")
	}
	boundary, ok := params["boundary"]
	if !ok || strings.TrimSpace(boundary) == "" {
		return registryPushRequest{}, fmt.Errorf("invalid multipart form")
	}

	reader := multipart.NewReader(r.Body, boundary)
	var tarFile *os.File
	defer func() {
		if err != nil && tarFile != nil {
			_ = tarFile.Close()
			_ = os.Remove(tarFile.Name())
		}
	}()

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return registryPushRequest{}, fmt.Errorf("invalid multipart form")
		}

		name := strings.TrimSpace(part.FormName())
		switch name {
		case "target":
			value, readErr := io.ReadAll(io.LimitReader(part, 4096))
			if readErr != nil {
				part.Close()
				return registryPushRequest{}, fmt.Errorf("invalid target field")
			}
			out.Target = strings.TrimSpace(string(value))
		case "scope":
			value, readErr := io.ReadAll(io.LimitReader(part, 64))
			if readErr != nil {
				part.Close()
				return registryPushRequest{}, fmt.Errorf("invalid scope field")
			}
			out.Scope, err = publishscope.Normalize(strings.TrimSpace(string(value)))
			if err != nil {
				part.Close()
				return registryPushRequest{}, err
			}
		case "image_tar":
			if tarFile != nil {
				part.Close()
				return registryPushRequest{}, fmt.Errorf("duplicate image upload")
			}
			tarFile, err = os.CreateTemp(registryPushTempDir(), "mcp-registry-upload-*.tar")
			if err != nil {
				part.Close()
				return registryPushRequest{}, fmt.Errorf("failed to store uploaded image")
			}
			if _, err := io.Copy(tarFile, part); err != nil {
				part.Close()
				return registryPushRequest{}, fmt.Errorf("failed to store uploaded image")
			}
		}
		part.Close()
	}

	if out.Target == "" {
		return registryPushRequest{}, fmt.Errorf("target is required")
	}
	if tarFile == nil {
		return registryPushRequest{}, fmt.Errorf("image_tar is required")
	}
	if err := tarFile.Close(); err != nil {
		return registryPushRequest{}, fmt.Errorf("failed to store uploaded image")
	}
	out.TarPath = tarFile.Name()
	return out, nil
}

func registryPushAuthContext(target string, scope publishscope.Scope, p principal) (namespace, teamSlug string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("target is required")
	}
	repoPath, err := imageRepositoryPath(target)
	if err != nil {
		return "", "", err
	}

	switch scope {
	case publishscope.Public:
		if p.Role != roleAdmin && !sharedCatalogWritableForUsers() {
			return "", "", fmt.Errorf("public catalog writes are restricted to admins")
		}
		return defaultPublicCatalogNamespace, "", nil
	case publishscope.Org:
		if p.Role != roleAdmin && !sharedCatalogWritableForUsers() {
			return "", "", fmt.Errorf("org catalog writes are restricted to admins")
		}
		return defaultOrgCatalogNamespace, "", nil
	case publishscope.Tenant, "":
		prefix, _, found := strings.Cut(repoPath, "/")
		if !found || strings.TrimSpace(prefix) == "" {
			return "", "", fmt.Errorf("tenant registry pushes require a team-scoped repository path")
		}
		if p.Role == roleAdmin {
			return "", prefix, nil
		}
		for _, team := range p.Teams {
			if prefix == strings.TrimSpace(team.Slug) || prefix == strings.TrimSpace(team.Namespace) {
				return strings.TrimSpace(team.Namespace), strings.TrimSpace(team.Slug), nil
			}
		}
		return "", "", fmt.Errorf("repository must be scoped to one of your teams (%s)", strings.Join(quoteStrings(principalRegistryScopes(p)), " or "))
	default:
		return "", "", fmt.Errorf("scope %q is invalid", scope)
	}
}

func imageRepositoryPath(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("target is required")
	}
	if idx := strings.LastIndex(image, "@"); idx > 0 {
		image = image[:idx]
	}
	if idx := strings.LastIndex(image, ":"); idx > 0 {
		suffix := image[idx+1:]
		if !strings.Contains(suffix, "/") {
			image = image[:idx]
		}
	}
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("target must include a registry/repository path")
	}
	if !imageReferenceHasRegistry(image) {
		return "", fmt.Errorf("target must include a registry host")
	}
	return strings.Join(parts[1:], "/"), nil
}

func principalRegistryScopes(p principal) []string {
	scopes := make([]string, 0, len(p.Teams)*2)
	for _, team := range p.Teams {
		if slug := strings.TrimSpace(team.Slug); slug != "" {
			scopes = append(scopes, slug)
		}
		if ns := strings.TrimSpace(team.Namespace); ns != "" {
			scopes = append(scopes, ns)
		}
	}
	return dedupeNonEmptyStrings(scopes)
}

func registryPushConfig(helperNamespace string) registrypush.Config {
	port := registrypush.ParsePortOrDefault(os.Getenv("MCP_REGISTRY_PORT"), registryPort)
	return registrypush.Config{
		HelperNamespace: helperNamespace,
		SkopeoImage:     strings.TrimSpace(os.Getenv("MCP_SKOPEO_IMAGE")),
		HelperTimeout:   5 * time.Minute,
		Hosts: registrypush.Hosts{
			InternalHostnames: registryPushInternalHostnames(),
			ServiceName:       registryServiceName,
			ServiceNamespace:  registryNamespace,
			ServicePort:       port,
		},
	}
}

func registryPushInternalHostnames() []string {
	hosts := []string{
		strings.TrimSpace(os.Getenv("MCP_REGISTRY_ENDPOINT")),
		strings.TrimSpace(os.Getenv("MCP_REGISTRY_INGRESS_HOST")),
		strings.TrimSpace(os.Getenv("MCP_REGISTRY_HOST")),
	}
	if domain := strings.TrimSpace(os.Getenv("MCP_PLATFORM_DOMAIN")); domain != "" {
		hosts = append(hosts, fmt.Sprintf("registry.%s", strings.TrimPrefix(domain, "registry.")))
	}
	if host := registryPullSecretHost(); host != "" {
		hosts = append(hosts, host)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	return out
}

func registryPushTempDir() string {
	return strings.TrimSpace(os.Getenv(registryPushTempDirEnv))
}
