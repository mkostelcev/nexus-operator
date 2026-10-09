package keycloak

// Role represents a Keycloak client role.
type Role struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// User represents a Keycloak user (partial).
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
}

// tokenResponse is the OAuth2 token response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// clientRepresentation is used to find client UUID by clientId.
type clientRepresentation struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
}
