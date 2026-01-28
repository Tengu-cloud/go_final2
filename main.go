package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Entry point
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <file.yaml>")
		os.Exit(1)
	}
	file := os.Args[1]

	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Printf("%s: cannot read file: %v\n", file, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Printf("%s: cannot parse YAML: %v\n", file, err)
		os.Exit(1)
	}

	errors := validatePod(file, &root)
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Println(e)
		}
		os.Exit(1)
	}
}

// Pod validation
func validatePod(filename string, root *yaml.Node) []string {
	var errs []string

	if len(root.Content) == 0 {
		return []string{fmt.Sprintf("%s: empty YAML file", filename)}
	}
	doc := root.Content[0]

	get := func(key string, node *yaml.Node) *yaml.Node {
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(node.Content)-1; i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k.Value == key {
				return v
			}
		}
		return nil
	}

	// apiVersion
	apiNode := get("apiVersion", doc)
	if apiNode == nil {
		errs = append(errs, "apiVersion is required")
	} else if apiNode.Kind != yaml.ScalarNode || apiNode.Value != "v1" {
		errs = append(errs, fmt.Sprintf("apiVersion has unsupported value '%s'", apiNode.Value))
	}

	// kind
	kindNode := get("kind", doc)
	if kindNode == nil {
		errs = append(errs, "kind is required")
	} else if kindNode.Kind != yaml.ScalarNode || kindNode.Value != "Pod" {
		errs = append(errs, fmt.Sprintf("kind has unsupported value '%s'", kindNode.Value))
	}

	// metadata
	metaNode := get("metadata", doc)
	if metaNode == nil {
		errs = append(errs, "metadata is required")
	} else {
		nameNode := get("name", metaNode)
		if nameNode == nil {
			errs = append(errs, "metadata.name is required")
		}
		// namespace optional
		labelsNode := get("labels", metaNode)
		if labelsNode != nil && labelsNode.Kind != yaml.MappingNode {
			errs = append(errs, "metadata.labels must be map")
		}
	}

	// spec
	specNode := get("spec", doc)
	if specNode == nil {
		errs = append(errs, "spec is required")
	} else {
		// os
		osNode := get("os", specNode)
		if osNode != nil {
			if osNode.Kind != yaml.ScalarNode {
				errs = append(errs, fmt.Sprintf("spec.os must be string"))
			} else if osNode.Value != "linux" && osNode.Value != "windows" {
				errs = append(errs, fmt.Sprintf("spec.os has unsupported value '%s'", osNode.Value))
			}
		}

		// containers
		containersNode := get("containers", specNode)
		if containersNode == nil {
			errs = append(errs, "spec.containers is required")
		} else if containersNode.Kind != yaml.SequenceNode {
			errs = append(errs, "spec.containers must be list")
		} else {
			for _, c := range containersNode.Content {
				errs = append(errs, validateContainer(filename, c)...)
			}
		}
	}

	return errs
}

// Container validation
func validateContainer(filename string, node *yaml.Node) []string {
	var errs []string

	get := func(key string, node *yaml.Node) *yaml.Node {
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i < len(node.Content)-1; i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k.Value == key {
				return v
			}
		}
		return nil
	}

	// name
	nameNode := get("name", node)
	if nameNode == nil {
		errs = append(errs, "container.name is required")
	} else {
		matched, _ := regexp.MatchString(`^[a-z0-9_]+$`, nameNode.Value)
		if !matched {
			errs = append(errs, fmt.Sprintf("container.name has invalid format '%s'", nameNode.Value))
		}
	}

	// image
	imageNode := get("image", node)
	if imageNode == nil {
		errs = append(errs, "container.image is required")
	} else {
		if !strings.HasPrefix(imageNode.Value, "registry.bigbrother.io/") || !strings.Contains(imageNode.Value, ":") {
			errs = append(errs, fmt.Sprintf("container.image has invalid format '%s'", imageNode.Value))
		}
	}

	// ports
	portsNode := get("ports", node)
	if portsNode != nil {
		if portsNode.Kind != yaml.SequenceNode {
			errs = append(errs, "container.ports must be list")
		} else {
			for _, p := range portsNode.Content {
				containerPortNode := get("containerPort", p)
				if containerPortNode == nil {
					errs = append(errs, "containerPort is required")
				} else {
					port, err := strconv.Atoi(containerPortNode.Value)
					if err != nil {
						errs = append(errs, fmt.Sprintf("containerPort must be int"))
					} else if port <= 0 || port >= 65536 {
						errs = append(errs, fmt.Sprintf("containerPort value out of range"))
					}
				}

				protoNode := get("protocol", p)
				if protoNode != nil && protoNode.Value != "TCP" && protoNode.Value != "UDP" {
					errs = append(errs, fmt.Sprintf("protocol has unsupported value '%s'", protoNode.Value))
				}
			}
		}
	}

	// probes
	for _, probeName := range []string{"readinessProbe", "livenessProbe"} {
		probeNode := get(probeName, node)
		if probeNode != nil {
			httpNode := get("httpGet", probeNode)
			if httpNode == nil {
				errs = append(errs, fmt.Sprintf("%s.httpGet is required", probeName))
			} else {
				pathNode := get("path", httpNode)
				portNode := get("port", httpNode)
				if pathNode == nil {
					errs = append(errs, fmt.Sprintf("%s.httpGet.path is required", probeName))
				} else if !strings.HasPrefix(pathNode.Value, "/") {
					errs = append(errs, fmt.Sprintf("%s.httpGet.path has invalid format '%s'", probeName, pathNode.Value))
				}
				if portNode == nil {
					errs = append(errs, fmt.Sprintf("%s.httpGet.port is required", probeName))
				} else {
					port, err := strconv.Atoi(portNode.Value)
					if err != nil || port <= 0 || port >= 65536 {
						errs = append(errs, fmt.Sprintf("%s.httpGet.port value out of range", probeName))
					}
				}
			}
		}
	}

	// resources
	resNode := get("resources", node)
	if resNode == nil {
		errs = append(errs, "resources is required")
	} else {
		for _, key := range []string{"limits", "requests"} {
			kv := get(key, resNode)
			if kv != nil {
				cpuNode := get("cpu", kv)
				memNode := get("memory", kv)
				if cpuNode != nil {
					if _, err := strconv.Atoi(cpuNode.Value); err != nil {
						errs = append(errs, fmt.Sprintf("resources.%s.cpu must be int", key))
					}
				}
				if memNode != nil {
					matched, _ := regexp.MatchString(`^\d+(Mi|Gi|Ki)$`, memNode.Value)
					if !matched {
						errs = append(errs, fmt.Sprintf("resources.%s.memory has invalid format '%s'", key, memNode.Value))
					}
				}
			}
		}
	}

	return errs
	// ваш код ниже

}
