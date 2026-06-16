package platformauth

import "context"

// ChainUserKeyResolvers tries resolvers in order until one recognizes the API key.
func ChainUserKeyResolvers(resolvers ...UserKeyResolver) UserKeyResolver {
	filtered := make([]UserKeyResolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		if resolver != nil {
			filtered = append(filtered, resolver)
		}
	}
	return chainedUserKeyResolver{resolvers: filtered}
}

type chainedUserKeyResolver struct {
	resolvers []UserKeyResolver
}

func (c chainedUserKeyResolver) ResolveAPIKey(ctx context.Context, rawKey string) (Principal, bool, error) {
	var lastErr error
	for _, resolver := range c.resolvers {
		principal, ok, err := resolver.ResolveAPIKey(ctx, rawKey)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return principal, true, nil
		}
	}
	return Principal{}, false, lastErr
}
