package manifest_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type kafkaManifestDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name         string `yaml:"name"`
					VolumeMounts []struct {
						Name      string `yaml:"name"`
						MountPath string `yaml:"mountPath"`
					} `yaml:"volumeMounts"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
		VolumeClaimTemplates []struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		} `yaml:"volumeClaimTemplates"`
	} `yaml:"spec"`
}

func TestDefaultKafkaPersistsBrokerData(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "k8s", "05-kafka.yaml"))
	if err != nil {
		t.Fatalf("read Kafka manifest: %v", err)
	}

	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	for {
		var doc kafkaManifestDoc
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode Kafka manifest: %v", err)
		}
		if doc.Kind != "StatefulSet" || doc.Metadata.Name != "kafka" {
			continue
		}
		claims := map[string]bool{}
		for _, claim := range doc.Spec.VolumeClaimTemplates {
			claims[claim.Metadata.Name] = true
		}
		if !claims["kafka-data"] {
			t.Fatal("Kafka missing volumeClaimTemplate kafka-data")
		}
		for _, container := range doc.Spec.Template.Spec.Containers {
			if container.Name != "kafka" {
				continue
			}
			mounts := map[string]string{}
			for _, mount := range container.VolumeMounts {
				mounts[mount.Name] = mount.MountPath
			}
			if mounts["kafka-data"] != "/var/lib/kafka/data" {
				t.Fatalf("Kafka data mount = %q, want /var/lib/kafka/data", mounts["kafka-data"])
			}
			return
		}
		t.Fatal("Kafka container not found")
	}
	t.Fatal("Kafka StatefulSet not found")
}
