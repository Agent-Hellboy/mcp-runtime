package runtimeapi

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
)

const publicBetaMaxReplicas int32 = 1

var (
	publicBetaServerResources = publishResourceProfile{
		defaultRequestCPU:    "50m",
		defaultRequestMemory: "64Mi",
		defaultLimitCPU:      "250m",
		defaultLimitMemory:   "256Mi",
		maxRequestCPU:        resource.MustParse("100m"),
		maxRequestMemory:     resource.MustParse("128Mi"),
		maxLimitCPU:          resource.MustParse("250m"),
		maxLimitMemory:       resource.MustParse("256Mi"),
	}
	publicBetaGatewayResources = publishResourceProfile{
		defaultRequestCPU:    "25m",
		defaultRequestMemory: "32Mi",
		defaultLimitCPU:      "100m",
		defaultLimitMemory:   "128Mi",
		maxRequestCPU:        resource.MustParse("50m"),
		maxRequestMemory:     resource.MustParse("64Mi"),
		maxLimitCPU:          resource.MustParse("100m"),
		maxLimitMemory:       resource.MustParse("128Mi"),
	}
)

type publishResourceProfile struct {
	defaultRequestCPU    string
	defaultRequestMemory string
	defaultLimitCPU      string
	defaultLimitMemory   string
	maxRequestCPU        resource.Quantity
	maxRequestMemory     resource.Quantity
	maxLimitCPU          resource.Quantity
	maxLimitMemory       resource.Quantity
}

func enforcePublishedServerSizePolicy(spec *mcpv1alpha1.MCPServerSpec, role string) error {
	if spec == nil || role == roleAdmin {
		return nil
	}
	if spec.Replicas == nil {
		replicas := publicBetaMaxReplicas
		spec.Replicas = &replicas
	} else if *spec.Replicas != publicBetaMaxReplicas {
		return fmt.Errorf("public beta MCPServers are limited to %d replica", publicBetaMaxReplicas)
	}
	if err := defaultAndValidatePublishResources("spec.resources", &spec.Resources, publicBetaServerResources); err != nil {
		return err
	}
	if spec.Gateway != nil && spec.Gateway.Enabled {
		if spec.Gateway.Resources == nil {
			spec.Gateway.Resources = &mcpv1alpha1.ResourceRequirements{}
		}
		if err := defaultAndValidatePublishResources("spec.gateway.resources", spec.Gateway.Resources, publicBetaGatewayResources); err != nil {
			return err
		}
	}
	return nil
}

func defaultAndValidatePublishResources(path string, resources *mcpv1alpha1.ResourceRequirements, profile publishResourceProfile) error {
	if resources.Requests == nil {
		resources.Requests = &mcpv1alpha1.ResourceList{}
	}
	if resources.Limits == nil {
		resources.Limits = &mcpv1alpha1.ResourceList{}
	}
	if strings.TrimSpace(resources.Requests.CPU) == "" {
		resources.Requests.CPU = profile.defaultRequestCPU
	}
	if strings.TrimSpace(resources.Requests.Memory) == "" {
		resources.Requests.Memory = profile.defaultRequestMemory
	}
	if strings.TrimSpace(resources.Limits.CPU) == "" {
		resources.Limits.CPU = profile.defaultLimitCPU
	}
	if strings.TrimSpace(resources.Limits.Memory) == "" {
		resources.Limits.Memory = profile.defaultLimitMemory
	}

	requestCPU, err := parsePublishQuantity(path+".requests.cpu", resources.Requests.CPU, profile.maxRequestCPU)
	if err != nil {
		return err
	}
	requestMemory, err := parsePublishQuantity(path+".requests.memory", resources.Requests.Memory, profile.maxRequestMemory)
	if err != nil {
		return err
	}
	limitCPU, err := parsePublishQuantity(path+".limits.cpu", resources.Limits.CPU, profile.maxLimitCPU)
	if err != nil {
		return err
	}
	limitMemory, err := parsePublishQuantity(path+".limits.memory", resources.Limits.Memory, profile.maxLimitMemory)
	if err != nil {
		return err
	}
	if requestCPU.Cmp(limitCPU) > 0 {
		return fmt.Errorf("%s.requests.cpu must not exceed %s.limits.cpu", path, path)
	}
	if requestMemory.Cmp(limitMemory) > 0 {
		return fmt.Errorf("%s.requests.memory must not exceed %s.limits.memory", path, path)
	}
	return nil
}

func parsePublishQuantity(path, value string, max resource.Quantity) (resource.Quantity, error) {
	quantity, err := resource.ParseQuantity(strings.TrimSpace(value))
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("%s must be a valid Kubernetes quantity", path)
	}
	if quantity.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("%s must be greater than zero", path)
	}
	if quantity.Cmp(max) > 0 {
		return resource.Quantity{}, fmt.Errorf("%s exceeds public beta maximum %s", path, max.String())
	}
	return quantity, nil
}
