// Package release builds and stores Warpgate release manifests.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
)

// Manifest captures the immutable inputs Warpgate instantiates during deploy.
type Manifest struct {
	// ID is the release identifier derived from image, compose, and environment inputs.
	ID string `json:"id"`
	// App is the application name.
	App string `json:"app"`
	// ImageRef is the image reference used in the generated compose override.
	ImageRef string `json:"image_ref"`
	// ImageTag is the mutable tag captured when the release was created.
	ImageTag string `json:"image_tag,omitempty"`
	// ImageDigest is the immutable digest captured when configured.
	ImageDigest string `json:"image_digest,omitempty"`
	// ComposeRev identifies the compose shape captured by this release.
	ComposeRev string `json:"compose_rev"`
	// EnvHash identifies the environment layer captured by this release.
	EnvHash string `json:"env_hash"`
	// Environment holds non-secret environment values written to the release env file.
	Environment map[string]string `json:"environment,omitempty"`
	// SecretsPrefix identifies the secret reference namespace for this release.
	SecretsPrefix string `json:"secrets_prefix,omitempty"`
	// Services holds per-service release inputs.
	Services map[string]ServiceManifest `json:"services,omitempty"`
	// CreatedAt is the time the release manifest was created.
	CreatedAt time.Time `json:"created_at"`
}

// ServiceManifest captures immutable release inputs for one Compose service.
type ServiceManifest struct {
	// ImageRef is the image reference used in the generated compose override.
	ImageRef string `json:"image_ref"`
	// ImageTag is the mutable tag captured when the release was created.
	ImageTag string `json:"image_tag,omitempty"`
	// ImageDigest is the immutable digest captured when configured.
	ImageDigest string `json:"image_digest,omitempty"`
	// EnvHash identifies the environment layer captured by this service release.
	EnvHash string `json:"env_hash"`
	// Environment holds non-secret environment values written to the service env file.
	Environment map[string]string `json:"environment,omitempty"`
	// SecretsPrefix identifies the secret reference namespace for this service.
	SecretsPrefix string `json:"secrets_prefix,omitempty"`
}

// Build creates a release manifest for an app and compose snapshot.
func Build(app *config.AppConfig, composeContent []byte, now time.Time) *Manifest {
	composeRev := app.ComposeRef
	if app.Source == nil {
		composeRev = "sha256:" + hashBytes(composeContent)
	}

	services := buildServices(app.EffectiveReleaseServices())
	envHash := combinedEnvHash(services)
	primary := primaryService(app.Name, services)
	manifest := &Manifest{
		App:           app.Name,
		ImageRef:      primary.ImageRef,
		ImageTag:      primary.ImageTag,
		ImageDigest:   primary.ImageDigest,
		ComposeRev:    composeRev,
		EnvHash:       envHash,
		Environment:   cloneEnvironment(primary.Environment),
		SecretsPrefix: primary.SecretsPrefix,
		Services:      services,
		CreatedAt:     now.UTC(),
	}
	manifest.ID = releaseID(manifest.ComposeRev, manifest.Services)
	return manifest
}

// Save writes a release manifest and updates latest.json for the app.
func Save(dir string, manifest *Manifest) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create releases directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal release manifest: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, manifest.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), data, 0644); err != nil {
		return fmt.Errorf("write latest release manifest: %w", err)
	}
	return nil
}

// ParseManifestJSON decodes a persisted release manifest.
func ParseManifestJSON(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse release manifest: %w", err)
	}
	return &manifest, nil
}

// Load reads a release manifest by ID or "latest".
func Load(dir, id string) (*Manifest, error) {
	if id == "" {
		return nil, fmt.Errorf("release id is required")
	}
	name := id
	if id != "latest" {
		name += ".json"
	} else {
		name = "latest.json"
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, fmt.Errorf("read release manifest %q: %w", id, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse release manifest %q: %w", id, err)
	}
	return &manifest, nil
}

func releaseID(composeRev string, services map[string]ServiceManifest) string {
	var data []byte
	data = append(data, "compose_rev="+composeRev+"\n"...)
	for _, name := range sortedServiceNames(services) {
		service := services[name]
		data = append(data, "service="+name+"\n"...)
		data = append(data, "image_ref="+service.ImageRef+"\n"...)
		data = append(data, "env_hash="+service.EnvHash+"\n"...)
	}
	return hashBytes(data)[:16]
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashEnvironment(env map[string]string, secretsPrefix string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var data []byte
	data = append(data, "secrets_prefix="+secretsPrefix+"\n"...)
	for _, key := range keys {
		data = append(data, key...)
		data = append(data, '=')
		data = append(data, env[key]...)
		data = append(data, '\n')
	}
	return "sha256:" + hashBytes(data)
}

func buildServices(services map[string]config.ReleaseServiceConfig) map[string]ServiceManifest {
	manifestServices := make(map[string]ServiceManifest, len(services))
	for name, service := range services {
		manifestServices[name] = ServiceManifest{
			ImageRef:      service.EffectiveImageRef(),
			ImageTag:      service.EffectiveImageTag(),
			ImageDigest:   service.ImageDigest,
			EnvHash:       hashEnvironment(service.Environment, service.SecretsPrefix),
			Environment:   cloneEnvironment(service.Environment),
			SecretsPrefix: service.SecretsPrefix,
		}
	}
	return manifestServices
}

func combinedEnvHash(services map[string]ServiceManifest) string {
	var data []byte
	for _, name := range sortedServiceNames(services) {
		data = append(data, name...)
		data = append(data, '=')
		data = append(data, services[name].EnvHash...)
		data = append(data, '\n')
	}
	return "sha256:" + hashBytes(data)
}

func primaryService(appName string, services map[string]ServiceManifest) ServiceManifest {
	if service, ok := services[appName]; ok {
		return service
	}
	names := sortedServiceNames(services)
	if len(names) == 0 {
		return ServiceManifest{}
	}
	return services[names[0]]
}

func sortedServiceNames(services map[string]ServiceManifest) []string {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneEnvironment(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	clone := make(map[string]string, len(env))
	for key, value := range env {
		clone[key] = value
	}
	return clone
}
