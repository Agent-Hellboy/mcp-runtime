FROM registry:2.8.3

# Match the uid/gid config/registry/base/deployment.yaml runs the registry as.
USER 1000:1000
