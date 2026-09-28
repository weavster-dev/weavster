package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestDockerCompose: docker-compose.yml and the server configuration it
// mounts agree (PostgreSQL 16, the DSN's host and credentials, the config
// path, the published port), and a server started from that configuration
// (on the test store, at a free port) accepts the compose file's admin
// password. The CI compose job runs the stack itself.
func TestDockerCompose(t *testing.T) {
	var compose struct {
		Services map[string]struct {
			Image       string            `yaml:"image"`
			Build       string            `yaml:"build"`
			Command     []string          `yaml:"command"`
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			Ports       []string          `yaml:"ports"`
			DependsOn   map[string]struct {
				Condition string `yaml:"condition"`
			} `yaml:"depends_on"`
		} `yaml:"services"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	db, server := compose.Services["db"], compose.Services["weavster"]
	cfg, err := serverconfig.Load(filepath.Join("..", "..", "docker", "weavster.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(cfg.Store.DSN)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := dsn.User.Password()
	_, port, _ := net.SplitHostPort(cfg.Listen.Address)
	configPath := ""
	if len(server.Command) == 3 {
		configPath = server.Command[2]
	}
	for _, c := range []struct{ what, got, want string }{
		{"db image", db.Image, "postgres:16-alpine"},
		{"weavster build", server.Build, "."},
		{"weavster waits for", server.DependsOn["db"].Condition, "service_healthy"},
		{"weavster command", strings.Join(server.Command, " "), "server --config " + configPath},
		{"weavster volumes", strings.Join(server.Volumes, ","), "./docker/weavster.yaml:" + configPath + ":ro"},
		{"weavster ports", strings.Join(server.Ports, ","), "127.0.0.1:8080:" + port},
		{"store dialect", cfg.Store.Dialect, serverconfig.DialectPostgres},
		{"DSN host", dsn.Hostname(), "db"},
		{"DSN user", dsn.User.Username(), db.Environment["POSTGRES_USER"]},
		{"DSN password", password, db.Environment["POSTGRES_PASSWORD"]},
		{"DSN database", strings.TrimPrefix(dsn.Path, "/"), db.Environment["POSTGRES_DB"]},
	} {
		if c.got != c.want || c.want == "" {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}

	// The same configuration on the test store and a free port.
	admin := server.Environment[envBootstrapPassword]
	t.Setenv(envBootstrapPassword, admin)
	addr := freeAddr(t)
	cfg.Listen.Address = addr
	cfg.Store.Dialect, cfg.Store.DSN = serverconfig.DialectMemory, ""
	if testPostgres() {
		cfg.Store.Dialect, cfg.Store.DSN = serverconfig.DialectPostgres, postgresStoreDSN(t)
	}
	local, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "weavster.yaml")
	if err := os.WriteFile(path, local, 0o600); err != nil {
		t.Fatal(err)
	}
	stop := startCLI(t, []string{"server", "--config", path}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, admin)); code != http.StatusOK {
		t.Errorf("the compose admin password: %d %s", code, body)
	}
}
