package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Resource struct {
	ID     string            `json:"ID"`
	Name   string            `json:"Name"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Image  string            `json:"Image"`
	Env    map[string]string `json:"Env"`
	Labels map[string]string `json:"Labels"`
	Ports  string            `json:"Ports"`
}
type Resources struct{ Runner CommandRunner }

func (r Resources) Run(ctx context.Context, args ...string) error {
	return r.Runner.Run(ctx, args, nil, io.Discard, io.Discard)
}
func (r Resources) Output(ctx context.Context, args ...string) (string, error) {
	var b strings.Builder
	err := r.Runner.Run(ctx, args, nil, &b, &b)
	return b.String(), err
}
func (r Resources) DockerAvailable(ctx context.Context) error {
	if err := r.Run(ctx, "version"); err != nil {
		return fmt.Errorf("Docker is unavailable: %w", err)
	}
	return nil
}
func (r Resources) Inspect(ctx context.Context, name string) (Resource, error) {
	out, err := r.Output(ctx, "inspect", name, "--format", "{{json .}}")
	if err != nil {
		return Resource{}, err
	}
	var raw struct {
		ID    string
		Name  string
		State struct {
			Status  string
			Running bool
		}
		Config struct {
			Image  string
			Env    []string
			Labels map[string]string
		}
		NetworkSettings struct {
			Ports map[string][]struct{ HostIP, HostPort string }
		}
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return Resource{}, err
	}
	env := map[string]string{}
	for _, value := range raw.Config.Env {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return Resource{ID: raw.ID, Name: strings.TrimPrefix(raw.Name, "/"), State: raw.State.Status, Image: raw.Config.Image, Env: env, Labels: raw.Config.Labels}, nil
}
func LabelsMatch(got map[string]string, expected map[string]string) bool {
	for k, v := range expected {
		if got[k] != v {
			return false
		}
	}
	return true
}
func Managed(labels map[string]string) bool { return labels["com.laradev.managed"] == "true" }
