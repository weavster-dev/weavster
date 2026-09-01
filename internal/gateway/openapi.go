package gateway

import (
	"crypto/tls"
	"errors"

	"gopkg.in/yaml.v3"
)

// openAPISpec is the served OpenAPI 3.1 contract (spec §5). It is the
// canonical contract published to agent-docs/openapi.yaml during P7.
const openAPISpec = `openapi: 3.1.0
info:
  title: Weavster API
  version: 0.1.0
  description: REST API for the Weavster message-oriented integration platform.
  license:
    name: Business Source License 1.1
servers:
  - url: https://localhost:8443
    description: Local HTTPS (production requires TLS)
security:
  - BasicAuth: []
components:
  securitySchemes:
    BasicAuth:
      type: http
      scheme: basic
      description: HTTP Basic Authentication. Credentials must only be sent over TLS.
paths:
  /api/v1/system:
    get:
      summary: System status
      security:
        - BasicAuth: []
      responses:
        "200":
          description: System information
        "401":
          description: Authentication required
        "403":
          description: Insufficient permissions
  /api/v1/topology:
    get:
      summary: Flow topology overview graph (read-only)
      security:
        - BasicAuth: []
      responses:
        "200":
          description: Overview graph
        "401":
          description: Authentication required
        "403":
          description: Insufficient permissions
  /api/v1/topology/flows/{flowId}:
    get:
      summary: Flow-internal topology graph (read-only)
      security:
        - BasicAuth: []
      parameters:
        - name: flowId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: Flow-internal graph
        "401":
          description: Authentication required
        "403":
          description: Insufficient permissions
  /api/v1/flows:
    get:
      summary: List flows
      security:
        - BasicAuth: []
      responses:
        "200": {description: Flow list}
        "401": {description: Authentication required}
        "403": {description: Insufficient permissions}
    post:
      summary: Create flow
      security:
        - BasicAuth: []
      responses:
        "201": {description: Created}
        "401": {description: Authentication required}
        "403": {description: Insufficient permissions}
  /api/v1/flows/{id}:
    get:
      summary: Get flow
      security:
        - BasicAuth: []
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "200": {description: Flow record}
        "401": {description: Authentication required}
        "403": {description: Insufficient permissions}
    delete:
      summary: Delete flow
      security:
        - BasicAuth: []
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      responses:
        "204": {description: Deleted}
        "401": {description: Authentication required}
        "403": {description: Insufficient permissions}
  /api/v1/messages:
    get:
      summary: Search messages
      security:
        - BasicAuth: []
      parameters:
        - name: status
          in: query
          schema: {type: string}
        - name: flowId
          in: query
          schema: {type: string}
      responses:
        "200": {description: Message search results}
        "401": {description: Authentication required}
        "403": {description: Insufficient permissions}
`

// OpenAPISpec returns the OpenAPI 3.1 contract.
func OpenAPISpec() string { return openAPISpec }

// ValidateSpec checks the served contract is well-formed YAML carrying the
// openapi + paths keys (spec §5).
func ValidateSpec() error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(openAPISpec), &doc); err != nil {
		return err
	}
	if _, ok := doc["openapi"]; !ok {
		return errors.New("gateway: openapi spec missing version")
	}
	if _, ok := doc["paths"]; !ok {
		return errors.New("gateway: openapi spec missing paths")
	}
	return nil
}

// ErrInvalidTLS is returned when a TLS configuration is unsatisfiable.
var ErrInvalidTLS = errors.New("gateway: invalid TLS configuration")

// TLSOptions configures the HTTPS listener (spec §2.13.44, §4.1).
type TLSOptions struct {
	MinVersion       uint16
	CipherSuites     []uint16
	CurvePreferences []tls.CurveID // ephemeral-DH group preference/sizing
}

// DefaultTLSOptions returns a hardened default: TLS 1.2+, strong AEAD ciphers,
// and modern ephemeral-DH curves.
func DefaultTLSOptions() TLSOptions {
	return TLSOptions{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
}

// BuildTLSConfig returns a *tls.Config from the options (spec §2.13.44).
func BuildTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if opts.MinVersion == 0 {
		return nil, ErrInvalidTLS
	}
	return &tls.Config{
		MinVersion:       opts.MinVersion,
		CipherSuites:     opts.CipherSuites,
		CurvePreferences: opts.CurvePreferences,
	}, nil
}
