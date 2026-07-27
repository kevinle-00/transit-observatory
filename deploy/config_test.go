package deploy_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type railwayConfig struct {
	Build struct {
		Builder        string `json:"builder"`
		DockerfilePath string `json:"dockerfilePath"`
	} `json:"build"`
	Deploy struct {
		PreDeployCommand string `json:"preDeployCommand"`
		StartCommand     string `json:"startCommand"`
		HealthcheckPath  string `json:"healthcheckPath"`
		CronSchedule     string `json:"cronSchedule"`
		RestartPolicy    string `json:"restartPolicyType"`
	} `json:"deploy"`
}

func readConfig(t *testing.T, name string) railwayConfig {
	t.Helper()
	data, err := os.ReadFile("railway/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var config railwayConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestRailwayServiceContracts(t *testing.T) {
	tests := []struct {
		name, dockerfile, start, preDeploy, health, cron, restart string
	}{
		{name: "api", dockerfile: "deploy/backend.Dockerfile", start: "/app/api", preDeploy: "/app/worker migrate", health: "/health", restart: "ON_FAILURE"},
		{name: "web", dockerfile: "deploy/web.Dockerfile", health: "/health", restart: "ON_FAILURE"},
		{name: "alerts-cron", dockerfile: "deploy/backend.Dockerfile", start: "/app/worker ingest-alerts", cron: "*/5 * * * *", restart: "NEVER"},
		{name: "gtfs-cron", dockerfile: "deploy/backend.Dockerfile", start: "/app/worker ingest-gtfs", cron: "15 17 * * *", restart: "NEVER"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := readConfig(t, test.name)
			if config.Build.Builder != "DOCKERFILE" || config.Build.DockerfilePath != test.dockerfile {
				t.Errorf("build = %+v", config.Build)
			}
			if config.Deploy.StartCommand != test.start || config.Deploy.PreDeployCommand != test.preDeploy ||
				config.Deploy.HealthcheckPath != test.health || config.Deploy.CronSchedule != test.cron ||
				config.Deploy.RestartPolicy != test.restart {
				t.Errorf("deploy = %+v", config.Deploy)
			}
		})
	}
}

func TestProductionServerAndImagesKeepDeploymentGuarantees(t *testing.T) {
	web, err := os.ReadFile("web.Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := os.ReadFile("backend.Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	caddy, err := os.ReadFile("Caddyfile")
	if err != nil {
		t.Fatal(err)
	}
	for label, content := range map[string]string{
		"web build-time API guard": string(web),
		"non-root web":             string(web),
		"non-root backend":         string(backend),
		"SPA fallback":             string(caddy),
		"web health endpoint":      string(caddy),
	} {
		var required string
		switch label {
		case "web build-time API guard":
			required = "VITE_API_BASE_URL build argument is required"
		case "non-root backend":
			required = "USER transit"
		case "non-root web":
			required = "USER web"
		case "SPA fallback":
			required = "try_files {path} /index.html"
		case "web health endpoint":
			required = "respond @health 200"
		}
		if !strings.Contains(content, required) {
			t.Errorf("%s missing %q", label, required)
		}
	}
}
