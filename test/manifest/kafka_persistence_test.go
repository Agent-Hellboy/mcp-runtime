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

func TestDefaultZooKeeperPersistsMetadata(t *testing.T) {
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
		if doc.Metadata.Name != "zookeeper" || doc.Kind == "Service" {
			continue
		}
		if doc.Kind != "StatefulSet" {
			t.Fatalf("ZooKeeper kind = %q, want StatefulSet", doc.Kind)
		}
		claims := map[string]bool{}
		for _, claim := range doc.Spec.VolumeClaimTemplates {
			claims[claim.Metadata.Name] = true
		}
		for _, want := range []string{"zookeeper-data", "zookeeper-log"} {
			if !claims[want] {
				t.Fatalf("ZooKeeper missing volumeClaimTemplate %q", want)
			}
		}
		for _, container := range doc.Spec.Template.Spec.Containers {
			if container.Name != "zookeeper" {
				continue
			}
			mounts := map[string]string{}
			for _, mount := range container.VolumeMounts {
				mounts[mount.Name] = mount.MountPath
			}
			if mounts["zookeeper-data"] != "/var/lib/zookeeper/data" {
				t.Fatalf("ZooKeeper data mount = %q", mounts["zookeeper-data"])
			}
			if mounts["zookeeper-log"] != "/var/lib/zookeeper/log" {
				t.Fatalf("ZooKeeper log mount = %q", mounts["zookeeper-log"])
			}
			return
		}
		t.Fatal("ZooKeeper container not found")
	}
	t.Fatal("ZooKeeper workload not found")
}
