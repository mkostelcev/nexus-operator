package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/keycloak"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

// ---------------------------------------------------------------------------
// Подставной Nexus
// ---------------------------------------------------------------------------

const (
	standNexusUser = "svc-operator"
	standNexusPass = "s3cret"
)

// nexusStand — подставной Nexus. Ответ вычисляется из метода, пути и заголовка
// авторизации, а не выдаётся константой: иначе тест не отличил бы «код удалил
// нужный объект» от «код удалил не тот объект» или «не удалил ничего».
// Коды повторяют настоящий Nexus: 204 на удаление существующего объекта,
// 404 на отсутствующий и на неизвестный путь, 401 без Basic-авторизации.
type nexusStand struct {
	*httptest.Server

	mu        sync.Mutex
	requests  []string          // "METHOD /path" в порядке поступления
	bodies    map[string]string // тело запроса по ключу "METHOD /path"
	existing  map[string]bool   // объекты, которые «есть» в Nexus
	realms    []string
	putRealms []string
}

// newNexusStand поднимает стенд. existing — пути, по которым объект существует;
// удаление всего остального даёт 404, как у настоящего Nexus.
func newNexusStand(t *testing.T, existing ...string) *nexusStand {
	t.Helper()
	s := &nexusStand{
		bodies:   map[string]string{},
		existing: map[string]bool{},
		realms:   []string{"NexusAuthenticatingRealm", "DockerToken"},
	}
	for _, p := range existing {
		s.existing[p] = true
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *nexusStand) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.requests = append(s.requests, key)
	s.bodies[key] = string(body)
	s.mu.Unlock()

	// Настоящий Nexus без авторизации отдаёт 401, а не молча обслуживает запрос.
	if u, p, ok := r.BasicAuth(); !ok || u != standNexusUser || p != standNexusPass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == nexus.SecurityRealmsActiveAPIPath:
		w.Header().Set("Content-Type", "application/json")
		s.mu.Lock()
		realms := append([]string(nil), s.realms...)
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(realms)

	case r.Method == http.MethodPut && r.URL.Path == nexus.SecurityRealmsActiveAPIPath:
		var got []string
		if err := json.Unmarshal(body, &got); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.putRealms = got
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodDelete:
		s.mu.Lock()
		ok := s.existing[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			// Объекта нет — ровно тот 404, который клиент превращает в ErrXNotFound.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *nexusStand) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *nexusStand) bodyOf(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[key]
}

// Логи клиента гасим, чтобы вывод теста оставался чистым.
func (s *nexusStand) client(t *testing.T) *nexus.Client {
	t.Helper()
	c, err := nexus.NewClient(s.URL, standNexusUser, standNexusPass)
	require.NoError(t, err)
	c.Logger.SetOutput(io.Discard)
	return c
}

// ---------------------------------------------------------------------------
// Подставной Keycloak
// ---------------------------------------------------------------------------

const (
	standKCRealm      = "nexus-realm"
	standKCClientName = "nexus"
	standKCClientUUID = "11111111-2222-3333-4444-555555555555"
	standKCToken      = "stand-access-token"
)

// keycloakStand повторяет три шага настоящего admin-API: выдать токен по
// client_credentials, найти UUID клиента по clientId, удалить роль.
// Запросы без Bearer-токена отвергаются с 401 — иначе тест не заметил бы,
// что клиент вообще не аутентифицировался.
type keycloakStand struct {
	*httptest.Server

	mu       sync.Mutex
	requests []string
}

func newKeycloakStand(t *testing.T) *keycloakStand {
	t.Helper()
	s := &keycloakStand{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *keycloakStand) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	s.mu.Unlock()

	tokenPath := fmt.Sprintf("/realms/%s/protocol/openid-connect/token", standKCRealm)
	adminPath := fmt.Sprintf("/admin/realms/%s", standKCRealm)

	if r.Method == http.MethodPost && r.URL.Path == tokenPath {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": standKCToken, "expires_in": 300,
		})
		return
	}

	// Всё под /admin требует ровно тот токен, который стенд выдал.
	if r.Header.Get("Authorization") != "Bearer "+standKCToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == adminPath+"/clients":
		if r.URL.Query().Get("clientId") != standKCClientName {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": standKCClientUUID, "clientId": standKCClientName},
		})

	case r.Method == http.MethodDelete && r.URL.Path == adminPath+"/clients/"+standKCClientUUID+"/roles/team-role-id":
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *keycloakStand) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *keycloakStand) client(t *testing.T) *keycloak.Client {
	t.Helper()
	c, err := keycloak.NewClient(s.URL, standKCRealm, standKCRealm, "operator", "secret", standKCClientName)
	require.NoError(t, err)
	return c
}

// ---------------------------------------------------------------------------
// finalize-пути: серия метрики должна исчезать вместе с CR
// ---------------------------------------------------------------------------

func finalizeTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, nexusv1alpha1.AddToScheme(scheme))
	return scheme
}

func finalizeFakeClient(t *testing.T, obj client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(finalizeTestScheme(t)).
		WithObjects(obj).
		WithStatusSubresource(obj).
		Build()
}

// finalizeObjectMeta — общая метаданная. Имя CR намеренно не совпадает с именем
// объекта в Nexus: так видно, что в метку name попадает имя ресурса в кластере,
// а в путь запроса — имя в Nexus.
func finalizeObjectMeta(name string) metav1.ObjectMeta {
	now := metav1.Now()
	return metav1.ObjectMeta{
		Name:              name,
		Namespace:         "nexus",
		DeletionTimestamp: &now,
		Finalizers:        []string{"finalizer.nexus.kostoed.ru"},
	}
}

type finalizeCase struct {
	typeLabel string
	crName    string
	// wantNexus — запросы, которые контроллер обязан отправить в Nexus.
	wantNexus []string
	// wantKeycloak — запросы в Keycloak. Пустой список значит «не ходить туда
	// вообще»: лишний поход в IAM из чужого контроллера — тоже поломка.
	wantKeycloak []string
	// wantPutBody — тело PUT, если контроллер что-то записывает.
	wantPutBody string
	// deletesInNexus — контроллер удаляет объект и обязан переживать ответ 404.
	deletesInNexus bool
	// run выполняет finalize и возвращает финализаторы CR после вызова.
	run func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string
}

func finalizeCases() []finalizeCase {
	return []finalizeCase{
		{
			typeLabel:      "contentselector",
			deletesInNexus: true,
			crName:         "cs-sample",
			wantNexus:      []string{"DELETE /service/rest/v1/security/content-selectors/team-selector"},
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.ContentSelector{
					ObjectMeta: finalizeObjectMeta("cs-sample"),
					Spec:       nexusv1alpha1.ContentSelectorSpec{Name: "team-selector"},
				}
				r := &ContentSelectorReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc},
				}
				_, err := r.finalizeContentSelector(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
		{
			typeLabel:      "privilege",
			deletesInNexus: true,
			crName:         "priv-sample",
			wantNexus:      []string{"DELETE /service/rest/v1/security/privileges/team-privilege"},
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.Privilege{
					ObjectMeta: finalizeObjectMeta("priv-sample"),
					Spec:       nexusv1alpha1.PrivilegeSpec{Name: "team-privilege"},
				}
				r := &PrivilegeReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc},
				}
				_, err := r.finalizePrivilege(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
		{
			typeLabel:      "routingrule",
			deletesInNexus: true,
			crName:         "rr-sample",
			wantNexus:      []string{"DELETE /service/rest/v1/routing-rules/team-rule"},
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.RoutingRule{
					ObjectMeta: finalizeObjectMeta("rr-sample"),
					Spec:       nexusv1alpha1.RoutingRuleSpec{Name: "team-rule"},
				}
				r := &RoutingRuleReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc},
				}
				_, err := r.finalizeRoutingRule(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
		{
			typeLabel:      "user",
			deletesInNexus: true,
			crName:         "user-sample",
			wantNexus:      []string{"DELETE /service/rest/v1/security/users/team-user"},
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.NexusUser{
					ObjectMeta: finalizeObjectMeta("user-sample"),
					Spec:       nexusv1alpha1.NexusUserSpec{UserId: "team-user"},
				}
				r := &NexusUserReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc},
				}
				_, err := r.finalizeUser(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
		{
			typeLabel:      "role",
			deletesInNexus: true,
			crName:         "role-sample",
			wantNexus:      []string{"DELETE /service/rest/v1/security/roles/team-role-id"},
			// Роль с keycloakSync=true обязана быть снята и в IAM: сначала токен по
			// client_credentials, затем поиск UUID клиента, затем удаление роли.
			wantKeycloak: []string{
				"POST /realms/" + standKCRealm + "/protocol/openid-connect/token",
				"GET /admin/realms/" + standKCRealm + "/clients",
				"DELETE /admin/realms/" + standKCRealm + "/clients/" + standKCClientUUID + "/roles/team-role-id",
			},
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.Role{
					ObjectMeta: finalizeObjectMeta("role-sample"),
					Spec: nexusv1alpha1.RoleSpec{
						RoleID: "team-role-id", Name: "Team Role", KeycloakSync: true,
					},
				}
				r := &RoleReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc, Keycloak: kc},
				}
				_, err := r.finalizeRole(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
		{
			typeLabel: "nexusconfiguration",
			crName:    "cfg-sample",
			// Этот контроллер ничего не удаляет, а сбрасывает Security Realms:
			// читает активные и записывает оставшиеся.
			wantNexus: []string{
				"GET /service/rest/v1/security/realms/active",
				"PUT /service/rest/v1/security/realms/active",
			},
			// Стенд отдаёт активными [NexusAuthenticatingRealm, DockerToken], CR владеет
			// только DockerToken — значит записаться обязан ровно остаток.
			wantPutBody: `["NexusAuthenticatingRealm"]`,
			run: func(t *testing.T, nc *nexus.Client, kc *keycloak.Client) []string {
				o := &nexusv1alpha1.NexusConfiguration{
					ObjectMeta: finalizeObjectMeta("cfg-sample"),
					Spec: nexusv1alpha1.NexusConfigurationSpec{
						SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
							Active: []string{"DockerToken"},
						},
					},
				}
				r := &NexusConfigurationReconciler{
					Client: finalizeFakeClient(t, o), Log: logr.Discard(),
					ExternalClients: ExternalClients{Nexus: nc},
				}
				_, err := r.finalizeNexusConfiguration(context.Background(), o, logr.Discard())
				require.NoError(t, err)
				return o.Finalizers
			},
		},
	}
}

// Шесть контроллеров удаляют серию nexus_operator_resource_ready в finalize-пути,
// и до сих пор ни один из них не был покрыт: путь упирался в живой Nexus.
// Клиент внедряется через ExternalClients и смотрит на httptest-стенд.
func TestFinalizeRemovesResourceReadyInEveryController(t *testing.T) {
	for _, tc := range finalizeCases() {
		t.Run(tc.typeLabel, func(t *testing.T) {
			opmetrics.ResourceReady.Reset()
			t.Cleanup(opmetrics.ResourceReady.Reset)

			// В стенде существуют ровно те объекты, которые контроллер обязан удалить.
			var existing []string
			for _, req := range tc.wantNexus {
				if len(req) > 7 && req[:7] == "DELETE " {
					existing = append(existing, req[7:])
				}
			}
			stand := newNexusStand(t, existing...)
			kcStand := newKeycloakStand(t)

			// Серия удаляемого CR и серия соседа, который обязан уцелеть.
			opmetrics.SetResourceReady(tc.typeLabel, "nexus", tc.crName, false)
			opmetrics.SetResourceReady(tc.typeLabel, "other", "survivor", true)

			finalizers := tc.run(t, stand.client(t), kcStand.client(t))

			// Финализатор снят — путь удаления пройден до конца.
			assert.NotContains(t, finalizers, "finalizer.nexus.kostoed.ru")
			// Контроллер сходил в Nexus по настоящему пути и за настоящим объектом.
			assert.Equal(t, tc.wantNexus, stand.got())
			// В Keycloak идут только те контроллеры, которым это положено.
			if len(tc.wantKeycloak) == 0 {
				assert.Empty(t, kcStand.got(), "контроллер не должен обращаться в Keycloak")
			} else {
				assert.Equal(t, tc.wantKeycloak, kcStand.got())
			}
			if tc.wantPutBody != "" {
				assert.JSONEq(t, tc.wantPutBody,
					stand.bodyOf("PUT "+nexus.SecurityRealmsActiveAPIPath))
			}
			// Серия удалена, сосед не задет.
			assertResourceReadySeries(t, fmt.Sprintf(
				`nexus_operator_resource_ready{name="survivor",resource_namespace="other",type=%q} 1`, tc.typeLabel))
		})
	}
}

// Если объект удалили в Nexus мимо оператора, удаление CR обязано пройти до конца:
// клиент превращает 404 в ErrXNotFound, а контроллер трактует это как «уже удалено».
// Без этого CR залипал бы с финализатором навсегда, а серия метрики — вместе с ним.
func TestFinalizeSucceedsWhenObjectAlreadyGoneInNexus(t *testing.T) {
	for _, tc := range finalizeCases() {
		if !tc.deletesInNexus {
			continue
		}
		t.Run(tc.typeLabel, func(t *testing.T) {
			opmetrics.ResourceReady.Reset()
			t.Cleanup(opmetrics.ResourceReady.Reset)

			// Стенд пуст: любое удаление получает настоящий 404 Nexus.
			stand := newNexusStand(t)
			kcStand := newKeycloakStand(t)

			opmetrics.SetResourceReady(tc.typeLabel, "nexus", tc.crName, false)
			opmetrics.SetResourceReady(tc.typeLabel, "other", "survivor", true)

			finalizers := tc.run(t, stand.client(t), kcStand.client(t))

			// Запрос всё равно был отправлен — оператор не пропускает удаление молча.
			assert.Equal(t, tc.wantNexus, stand.got())
			assert.NotContains(t, finalizers, "finalizer.nexus.kostoed.ru")
			assertResourceReadySeries(t, fmt.Sprintf(
				`nexus_operator_resource_ready{name="survivor",resource_namespace="other",type=%q} 1`, tc.typeLabel))
		})
	}
}

// Девятый контроллер: ветка удаления живёт прямо в Reconcile и упирается только
// в Keycloak. Свой финализатор (не общий "finalizer.nexus.kostoed.ru"), поэтому
// кейс отдельный. Ошибки очистки привязок контроллер только логирует, так что
// стенду достаточно выдать токен — снятие финализатора от этого не зависит.
func TestNexusTeamBindingFinalizeRemovesResourceReady(t *testing.T) {
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	kcStand := newKeycloakStand(t)
	now := metav1.Now()
	binding := &nexusv1alpha1.NexusTeamBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "binding-sample",
			Namespace:         "nexus",
			DeletionTimestamp: &now,
			Finalizers:        []string{bindingFinalizer},
		},
	}
	r := &NexusTeamBindingReconciler{
		Client:          finalizeFakeClient(t, binding),
		Log:             logr.Discard(),
		ExternalClients: ExternalClients{Keycloak: kcStand.client(t)},
	}

	opmetrics.SetResourceReady("nexusteambinding", "nexus", "binding-sample", false)
	opmetrics.SetResourceReady("nexusteambinding", "other", "survivor", true)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(binding),
	})
	require.NoError(t, err)

	// В Keycloak запросов нет, и это правильно: привязок в Spec нет, чистить нечего,
	// а токен клиент берёт лениво. Что внедрённый клиент реально используется,
	// доказывает мутация «keycloakClient() игнорирует поле»: тогда Reconcile уходит
	// в ветку ошибки, ставит метрику в 0 вместо удаления, и тест падает.
	assert.Empty(t, kcStand.got())

	// Финализатор снят: после снятия последнего объект исчезает из fake-клиента.
	var after nexusv1alpha1.NexusTeamBinding
	if getErr := r.Get(context.Background(), client.ObjectKeyFromObject(binding), &after); getErr == nil {
		assert.NotContains(t, after.Finalizers, bindingFinalizer)
	} else {
		require.True(t, k8serrors.IsNotFound(getErr), "неожиданная ошибка чтения CR: %v", getErr)
	}

	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="survivor",resource_namespace="other",type="nexusteambinding"} 1`)
}
