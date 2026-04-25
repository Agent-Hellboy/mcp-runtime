# api/v1alpha1

## doc.go
- L1-L8: Package comment and `package v1alpha1` declaration marking the API group version.

## groupversion_info.go
- L1-L9: Imports kubernetes scheme metadata utilities.
- L11-L20: Defines group/version constants (`GroupName` and `SchemeGroupVersion`), a `SchemeBuilder`, and adds the `AddToScheme` helper used by the operator manager.

## register.go
- L1-L6: Boilerplate required by kubebuilder for code generation; imports the package and triggers registration through init.
- L8-L13: `init` registers the MCPServer types with the scheme builder.

## mcpserver_types.go
- L1: Package declaration.
- L3-L7: Imports Kubernetes metav1 for metadata types.
- L9: kubebuilder object generation marker.
- L11-L62: `MCPServerSpec` struct fields with JSON tags and comments describing image, registry overrides, pull secrets, scaling, ports, ingress settings, resources, and environment variables.
- L64-L75: `ResourceRequirements` struct with optional limit/request lists.
- L77-L84: `ResourceList` struct for CPU/memory strings.
- L86-L93: `EnvVar` struct mirroring name/value pairs for pod env.
- L95-L123: `MCPServerStatus` struct capturing phase, message, conditions, and readiness booleans.
- L125-L138: `Condition` struct storing typed condition info with transition time, reason, and message.
- L140-L153: `MCPServer` root type with embedded metadata, spec, and status plus kubebuilder printcolumn annotations for kubectl tables.
- L155-L161: `MCPServerList` slice wrapper for list operations.

## access_types.go
- L1-L3: Package declaration and metav1 import for `Time` and `Condition` types.
- L5-L10: `ServerReference` — name + optional namespace, used by both grants and sessions to identify the target `MCPServer`.
- L12-L17: `SubjectRef` — `humanID` and optional `agentID`. Both are optional in JSON; controllers/UIs are expected to require at least one.
- L19-L25: `ToolRule` — per-tool decision (`PolicyDecision` enum from `validation.go`) plus optional `requiredTrust`. Used inside grants to override or augment a server's tool inventory rules.
- L27-L36: `MCPAccessGrantSpec` — pairs a server with a subject. Carries `MaxTrust` (the admin ceiling), an optional `PolicyVersion` for cache-busting, a `Disabled` boolean (the soft-off switch the gateway honors), and a list of `ToolRules`.
- L38-L44: `MCPAccessGrantStatus` — phase + message + standard `metav1.Condition` slice for the operator to reflect reconciliation state.
- L46-L62: `MCPAccessGrant` root type with kubebuilder printcolumns surfacing Server / Human / Agent / Trust / Disabled / Age in `kubectl get mcpaccessgrant`.
- L64-L71: `MCPAccessGrantList` slice wrapper for list operations.
- L73-L83: `MCPAgentSessionSpec` — server + subject, plus `ConsentedTrust`, optional `ExpiresAt` (`*metav1.Time`), a `Revoked` flag (the gateway honors this immediately), an optional `UpstreamTokenSecretRef` for upstream credentials, and a `PolicyVersion`.
- L85-L91: `MCPAgentSessionStatus` — same phase/message/conditions shape as grants.
- L93-L110: `MCPAgentSession` root type with kubebuilder printcolumns: Server / Human / Agent / Trust / Revoked / Expires / Age.
- L112-L119: `MCPAgentSessionList` slice wrapper.

## validation.go
- Defines the `PolicyDecision` and `TrustLevel` enums plus shared helpers used by both the operator and the access types above. `validation_test.go` covers the input shapes.

## zz_generated.deepcopy.go
- Auto-generated deep-copy implementations for every API struct to satisfy Kubernetes runtime interfaces. Functions allocate new objects, copy fields, and implement `DeepCopyObject` so controller-runtime can safely duplicate MCPServer, MCPAccessGrant, and MCPAgentSession resources.
