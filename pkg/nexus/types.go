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
	Privileges  []string `json:"privileges"`
	Roles       []string `json:"roles"`
}
