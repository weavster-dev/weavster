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
paths:
  /api/v1/auth/login:
    post:
      summary: Log in and receive a bearer token (valid 12 hours)
      security:
        - csrfMarker: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [username, password]
              properties:
                username: {type: string}
                password: {type: string}
                mfaCode: {type: string}
      responses:
        "200": {description: "Token, expiresAt, and user"}
        "401": {description: Invalid username or password}
  /api/v1/auth/logout:
    post:
      summary: Revoke the bearer token used for this request
      responses:
        "204": {description: Logged out}
  /api/v1/auth/me:
    get:
      summary: Current user, permissions, and whether a password change is required
      responses:
        "200": {description: Current user}
  /api/v1/auth/password:
    post:
      summary: Change the current user's password
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [oldPassword, newPassword]
              properties:
                oldPassword: {type: string}
                newPassword: {type: string}
      responses:
        "204": {description: Password changed}
        "400": {description: New password rejected by the password policy}
  /api/v1/system:
    get:
      summary: System status
      responses:
        "200":
          description: System information
  /api/v1/topology:
    get:
      summary: Flow topology overview graph (read-only)
      responses:
        "200":
          description: Overview graph
  /api/v1/topology/flows/{flowId}:
    get:
      summary: Flow-internal topology graph (read-only)
      parameters:
        - name: flowId
          in: path
          required: true
          schema: {type: string}
      responses:
        "200":
          description: Flow-internal graph
  /api/v1/flows:
    get:
      summary: List flows
      responses:
        "200": {description: Flow list}
    post:
      summary: Create flow
      responses:
        "201": {description: Created}
  /api/v1/flows/{id}:
    get:
      summary: Get a flow (requires flows:view)
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: The flow}
        "404": {description: Unknown flow}
    delete:
      summary: Delete a flow (requires flows:edit)
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "204": {description: Deleted}
        "404": {description: Unknown flow}
    put:
      summary: Replace a flow's definition; status is kept (requires flows:edit)
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: object}
      responses:
        "200": {description: The updated flow}
        "400": {description: Invalid definition, a status field, or a different id in the body}
        "404": {description: Unknown flow}
  /api/v1/flows/{id}/enable:
    post:
      summary: Make a flow eligible for auto-deploy at startup (requires flows:edit)
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: The updated flow}
        "404": {description: Unknown flow}
  /api/v1/flows/{id}/disable:
    post:
      summary: Remove a flow's auto-deploy eligibility (requires flows:edit)
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200": {description: The updated flow}
        "404": {description: Unknown flow}
  /api/v1/flows/{id}/{action}:
    post:
      summary: Change a flow's lifecycle state (requires flows:deploy)
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
        - name: action
          in: path
          required: true
          schema: {type: string, enum: [deploy, undeploy, start, stop, pause, halt, resume]}
      responses:
        "200": {description: The updated flow}
        "404": {description: Unknown flow or action}
        "409": {description: "Transition not allowed from the flow's status"}
  /api/v1/flows/redeploy-all:
    post:
      summary: Undeploy and re-deploy every flow that is not undeployed (deployed, started, paused, halted, stopped); each ends deployed (requires flows:deploy)
      responses:
        "200": {description: The redeployed flows}
        "500": {description: "Stopped part-way: {error: {code: REDEPLOY_INCOMPLETE}, redeployed: [flows already redeployed]}"}
  /api/v1/flows/{id}/messages:
    post:
      summary: Send a message into a flow (requires messages:send)
      description: >
        Persists the message, runs the flow's transform, and delivers it to each destination.
        The body must be a single JSON object when the flow has a transform; passthrough
        flows (no transform) accept any bytes.
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: object}
          application/octet-stream:
            schema: {type: string, format: binary}
      responses:
        "202": {description: "Processed: {id, status} with status sent, queued, dead-lettered, filtered, or errored"}
        "400": {description: The flow has a transform and the body is not a single JSON object, or the body could not be read}
        "404": {description: Unknown flow}
        "409": {description: The flow is not started}
        "413": {description: Body larger than 10 MiB}
  /api/v1/flows/{id}/stats:
    get:
      summary: Flow message counters (requires flows:view)
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
        - name: lifetime
          in: query
          schema: {type: boolean}
      responses:
        "200": {description: "received, filtered, transformed, sent, errored, queued, destinations, lastMessageAt"}
        "400": {description: lifetime is not a boolean}
        "404": {description: Unknown flow}
  /api/v1/events:
    get:
      summary: Search the event log (requires events:view)
      parameters:
        - name: type
          in: query
          schema: {type: string}
        - name: flowId
          in: query
          schema: {type: string}
        - name: limit
          in: query
          description: Newest N matching events (1-10000, default 1000)
          schema: {type: integer, minimum: 1, maximum: 10000}
      responses:
        "200": {description: "Events, oldest first: id, at, type, flowId, data"}
        "400": {description: Invalid limit}
  /api/v1/messages:
    get:
      summary: Search messages
      responses:
        "200": {description: Message search results}
security:
  - basicAuth: []
    csrfMarker: []
  - bearerAuth: []
    csrfMarker: []
components:
  securitySchemes:
    basicAuth: {type: http, scheme: basic}
    bearerAuth: {type: http, scheme: bearer}
    csrfMarker: {type: apiKey, in: header, name: X-Weavster-CSRF}
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
