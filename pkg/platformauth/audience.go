package platformauth

import "mcp-runtime/pkg/serviceutil"

const (
	AudiencePlatform  = "platform"
	AudienceRuntime   = "runtime-control"
	AudienceAnalytics = "analytics-api"
)

func RequiredAudiences() []string {
	return []string{AudiencePlatform, AudienceRuntime, AudienceAnalytics}
}

func audienceMatches(aud any, expected string) bool {
	return serviceutil.AudienceMatches(aud, expected)
}
