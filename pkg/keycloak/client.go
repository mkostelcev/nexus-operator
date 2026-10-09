package keycloak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

var (
	ErrMissingEnvVars    = errors.New("missing required Keycloak environment variables")
	ErrUnexpectedStatus  = errors.New("unexpected response status")
	ErrClientNotFound    = errors.New("keycloak client not found")
	ErrUserNotFound      = errors.New("keycloak user not found")
	ErrRoleNotFound      = errors.New("keycloak client role not found")
	ErrRoleAlreadyExists = errors.New("keycloak client role already exists")
	ErrTokenExpired      = errors.New("failed to obtain access token")

	clientInstance *Client
	initOnce       sync.Once
	initError      error
)

// Client is a Keycloak Admin REST API client.
type Client struct {
	resty        *resty.Client
	logger       *logrus.Logger
	baseURL      string
	realm        string
	authRealm    string // realm for token endpoint (usually "master" or same as realm)
	clientID     string // client_id for auth
	clientSecret string
	nexusClient  string // Keycloak client name for role management ("nexus")

	mu          sync.Mutex
	clientUUID  string // cached UUID of the nexus client
	token       string
	tokenExpiry time.Time
}

// GetClient returns the global Keycloak client singleton.
func GetClient() (*Client, error) {
	initOnce.Do(func() {
		clientInstance, initError = newClientFromEnv()
	})
	if initError != nil {
		return nil, initError
	}
	return clientInstance, nil
}

func newClientFromEnv() (*Client, error) {
	baseURL := os.Getenv("KEYCLOAK_URL")
	realm := os.Getenv("KEYCLOAK_REALM")
	clientID := os.Getenv("KEYCLOAK_CLIENT_ID")
	clientSecret := os.Getenv("KEYCLOAK_CLIENT_SECRET")
	nexusClient := os.Getenv("KEYCLOAK_NEXUS_CLIENT")

	if baseURL == "" || realm == "" || clientID == "" || clientSecret == "" || nexusClient == "" {
		return nil, fmt.Errorf(
			"%w: KEYCLOAK_URL, KEYCLOAK_REALM, KEYCLOAK_CLIENT_ID, "+
				"KEYCLOAK_CLIENT_SECRET, KEYCLOAK_NEXUS_CLIENT",
			ErrMissingEnvVars,
		)
	}

	authRealm := os.Getenv("KEYCLOAK_AUTH_REALM")
	if authRealm == "" {
		authRealm = realm
	}

	return NewClient(baseURL, realm, authRealm, clientID, clientSecret, nexusClient)
}

// NewClient creates a new Keycloak client.
func NewClient(baseURL, realm, authRealm, clientID, clientSecret, nexusClient string) (*Client, error) {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(30 * time.Second)

	return &Client{
		resty:        r,
		logger:       logger,
		baseURL:      baseURL,
		realm:        realm,
		authRealm:    authRealm,
		clientID:     clientID,
		clientSecret: clientSecret,
		nexusClient:  nexusClient,
	}, nil
}

// ensureToken obtains or refreshes the access token.
func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}

	resp, err := c.resty.R().
		SetContext(ctx).
		SetFormData(map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     c.clientID,
			"client_secret": c.clientSecret,
		}).
		Post(fmt.Sprintf("/realms/%s/protocol/openid-connect/token", c.authRealm))
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("%w: token endpoint returned %d: %s", ErrTokenExpired, resp.StatusCode(), resp.String())
	}

	var tok tokenResponse
	if err := json.Unmarshal(resp.Body(), &tok); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	c.token = tok.AccessToken
	// Refresh 30 seconds before expiry.
	c.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn-30) * time.Second)
	return nil
}

// authRequest creates an authenticated resty request.
func (c *Client) authRequest(ctx context.Context) (*resty.Request, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()

	return c.resty.R().
		SetContext(ctx).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json"), nil
}

// adminPath returns the admin API base path.
func (c *Client) adminPath() string {
	return fmt.Sprintf("/admin/realms/%s", c.realm)
}

// resolveClientUUID fetches and caches the internal UUID of the nexus client.
func (c *Client) resolveClientUUID(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.clientUUID != "" {
		uuid := c.clientUUID
		c.mu.Unlock()
		return uuid, nil
	}
	c.mu.Unlock()

	req, err := c.authRequest(ctx)
	if err != nil {
		return "", err
	}

	resp, err := req.Get(fmt.Sprintf("%s/clients?clientId=%s", c.adminPath(), c.nexusClient))
	if err != nil {
		return "", fmt.Errorf("list clients: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("%w: list clients returned %d", ErrUnexpectedStatus, resp.StatusCode())
	}

	var clients []clientRepresentation
	if err := json.Unmarshal(resp.Body(), &clients); err != nil {
		return "", fmt.Errorf("parse clients: %w", err)
	}

	for _, cl := range clients {
		if cl.ClientID == c.nexusClient {
			c.mu.Lock()
			c.clientUUID = cl.ID
			c.mu.Unlock()
			return cl.ID, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrClientNotFound, c.nexusClient)
}

// ListClientRoles lists all client roles for the nexus client.
func (c *Client) ListClientRoles(ctx context.Context) ([]Role, error) {
	uuid, err := c.resolveClientUUID(ctx)
	if err != nil {
		return nil, err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := req.Get(fmt.Sprintf("%s/clients/%s/roles", c.adminPath(), uuid))
	if err != nil {
		return nil, fmt.Errorf("list client roles: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: list roles returned %d", ErrUnexpectedStatus, resp.StatusCode())
	}

	var roles []Role
	if err := json.Unmarshal(resp.Body(), &roles); err != nil {
		return nil, fmt.Errorf("parse roles: %w", err)
	}
	return roles, nil
}

// EnsureClientRole creates a client role if it doesn't exist. Returns nil if already exists.
func (c *Client) EnsureClientRole(ctx context.Context, roleName, description string) error {
	uuid, err := c.resolveClientUUID(ctx)
	if err != nil {
		return err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return err
	}

	body := Role{Name: roleName, Description: description}
	resp, err := req.
		SetBody(body).
		Post(fmt.Sprintf("%s/clients/%s/roles", c.adminPath(), uuid))
	if err != nil {
		return fmt.Errorf("create client role %s: %w", roleName, err)
	}

	switch resp.StatusCode() {
	case http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusConflict:
		// Role already exists — ok.
		return nil
	default:
		return fmt.Errorf("%w: create role %s returned %d: %s",
			ErrUnexpectedStatus, roleName,
			resp.StatusCode(), resp.String())
	}
}

// UpdateClientRole updates an existing client role (e.g. description).
func (c *Client) UpdateClientRole(ctx context.Context, roleName string, role Role) error {
	uuid, err := c.resolveClientUUID(ctx)
	if err != nil {
		return err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return err
	}

	resp, err := req.
		SetBody(role).
		Put(fmt.Sprintf("%s/clients/%s/roles/%s", c.adminPath(), uuid, roleName))
	if err != nil {
		return fmt.Errorf("update client role %s: %w", roleName, err)
	}

	switch resp.StatusCode() {
	case http.StatusNoContent, http.StatusOK:
		return nil
	default:
		return fmt.Errorf("%w: update role %s returned %d: %s",
			ErrUnexpectedStatus, roleName,
			resp.StatusCode(), resp.String())
	}
}

// DeleteClientRole deletes a client role.
func (c *Client) DeleteClientRole(ctx context.Context, roleName string) error {
	uuid, err := c.resolveClientUUID(ctx)
	if err != nil {
		return err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return err
	}

	resp, err := req.Delete(fmt.Sprintf("%s/clients/%s/roles/%s", c.adminPath(), uuid, roleName))
	if err != nil {
		return fmt.Errorf("delete client role %s: %w", roleName, err)
	}

	switch resp.StatusCode() {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return nil // Already gone.
	default:
		return fmt.Errorf("%w: delete role %s returned %d: %s",
			ErrUnexpectedStatus, roleName,
			resp.StatusCode(), resp.String())
	}
}

// GetClientRoleByName fetches a single client role by name.
func (c *Client) GetClientRoleByName(ctx context.Context, roleName string) (*Role, error) {
	uuid, err := c.resolveClientUUID(ctx)
	if err != nil {
		return nil, err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := req.Get(fmt.Sprintf("%s/clients/%s/roles/%s", c.adminPath(), uuid, roleName))
	if err != nil {
		return nil, fmt.Errorf("get client role %s: %w", roleName, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrRoleNotFound, roleName)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: get role returned %d", ErrUnexpectedStatus, resp.StatusCode())
	}

	var role Role
	if err := json.Unmarshal(resp.Body(), &role); err != nil {
		return nil, fmt.Errorf("parse role: %w", err)
	}
	return &role, nil
}

// GetUserByUsername finds a user by username. Returns the first exact match.
func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	req, err := c.authRequest(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := req.
		SetQueryParam("username", username).
		SetQueryParam("exact", "true").
		Get(fmt.Sprintf("%s/users", c.adminPath()))
	if err != nil {
		return nil, fmt.Errorf("search user by username %s: %w", username, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: search users returned %d", ErrUnexpectedStatus, resp.StatusCode())
	}

	var users []User
	if err := json.Unmarshal(resp.Body(), &users); err != nil {
		return nil, fmt.Errorf("parse users: %w", err)
	}

	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUserNotFound, username)
}

// GetUserClientRoles returns client roles assigned to a user for the nexus client.
func (c *Client) GetUserClientRoles(ctx context.Context, userUUID string) ([]Role, error) {
	clientUUID, err := c.resolveClientUUID(ctx)
	if err != nil {
		return nil, err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := req.Get(fmt.Sprintf("%s/users/%s/role-mappings/clients/%s", c.adminPath(), userUUID, clientUUID))
	if err != nil {
		return nil, fmt.Errorf("get user client roles: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: get user roles returned %d", ErrUnexpectedStatus, resp.StatusCode())
	}

	var roles []Role
	if err := json.Unmarshal(resp.Body(), &roles); err != nil {
		return nil, fmt.Errorf("parse user roles: %w", err)
	}
	return roles, nil
}

// AssignClientRoles assigns client roles to a user.
func (c *Client) AssignClientRoles(ctx context.Context, userUUID string, roles []Role) error {
	clientUUID, err := c.resolveClientUUID(ctx)
	if err != nil {
		return err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return err
	}

	resp, err := req.
		SetBody(roles).
		Post(fmt.Sprintf("%s/users/%s/role-mappings/clients/%s", c.adminPath(), userUUID, clientUUID))
	if err != nil {
		return fmt.Errorf("assign client roles: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("%w: assign roles returned %d: %s", ErrUnexpectedStatus, resp.StatusCode(), resp.String())
	}
	return nil
}

// UnassignClientRoles removes client roles from a user.
func (c *Client) UnassignClientRoles(ctx context.Context, userUUID string, roles []Role) error {
	clientUUID, err := c.resolveClientUUID(ctx)
	if err != nil {
		return err
	}

	req, err := c.authRequest(ctx)
	if err != nil {
		return err
	}

	resp, err := req.
		SetBody(roles).
		Delete(fmt.Sprintf("%s/users/%s/role-mappings/clients/%s", c.adminPath(), userUUID, clientUUID))
	if err != nil {
		return fmt.Errorf("unassign client roles: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("%w: unassign roles returned %d: %s", ErrUnexpectedStatus, resp.StatusCode(), resp.String())
	}
	return nil
}
