package nexus

// Добавим структуру для запроса content-selector
type ContentSelectorRequest struct {
	Name        string
	Description string
	Expression  string
}

// Добавим структуру для ответа content-selector
type ContentSelectorResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
}

// Структура для работы с ролями
type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Privileges  []string `json:"privileges"`
	Roles       []string `json:"roles"`
}

// RepositoryListItem — элемент списка репозиториев из GET /v1/repositories
type RepositoryListItem struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Type   string `json:"type"`
	URL    string `json:"url"`
}

// PrivilegeListItem — элемент списка привилегий из GET /v1/security/privileges
type PrivilegeListItem struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"readOnly"`
}

// User — пользователь Nexus из REST API
type User struct {
	UserId        string   `json:"userId"`
	FirstName     string   `json:"firstName"`
	LastName      string   `json:"lastName"`
	EmailAddress  string   `json:"emailAddress"`
	Source        string   `json:"source"`
	Status        string   `json:"status"`
	ReadOnly      bool     `json:"readOnly"`
	Roles         []string `json:"roles"`
	ExternalRoles []string `json:"externalRoles,omitempty"`
}

// UserCreateRequest — запрос на создание пользователя в Nexus
type UserCreateRequest struct {
	UserId       string   `json:"userId"`
	FirstName    string   `json:"firstName"`
	LastName     string   `json:"lastName"`
	EmailAddress string   `json:"emailAddress"`
	Password     string   `json:"password"`
	Status       string   `json:"status"`
	Roles        []string `json:"roles"`
}

// UserUpdateRequest — запрос на обновление пользователя в Nexus (без пароля)
type UserUpdateRequest struct {
	UserId       string   `json:"userId"`
	FirstName    string   `json:"firstName"`
	LastName     string   `json:"lastName"`
	EmailAddress string   `json:"emailAddress"`
	Status       string   `json:"status"`
	Roles        []string `json:"roles"`
}
