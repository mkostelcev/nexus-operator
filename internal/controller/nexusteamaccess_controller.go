package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	teamAccessFinalizer    = "finalizer.nexus.kostoed.ru"
	teamAccessRequeueDelay = 30 * time.Second
	teamAccessResyncPeriod = 15 * time.Minute

	labelTeamAccess = "nexus.kostoed.ru/team-access"
	labelManagedBy  = "nexus.kostoed.ru/managed-by"
	managedByValue  = "nexusteamaccess-controller"
)

var (
	errUnknownAccessLevel = errors.New("неизвестный accessLevel")
	errUnsupportedFormat  = errors.New("неподдерживаемый формат")
)

// Access level action mappings.
var accessLevelActions = map[string][]string{
	"ro":  {"READ", "BROWSE"},
	"rw":  {"READ", "BROWSE", "ADD", "EDIT"},
	"rwd": {"READ", "BROWSE", "ADD", "EDIT", "DELETE"},
}

// accessLevelHierarchy defines which access level includes which as subrole.
var accessLevelHierarchy = map[string]string{
	"rw":  "ro",
	"rwd": "rw",
}

type NexusTeamAccessReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses/finalizers,verbs=update
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=contentselectors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=privileges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=roles,verbs=get;list;watch;create;update;patch;delete

func (r *NexusTeamAccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("nexusteamaccess", req.NamespacedName)
	log.V(1).Info("Начало обработки NexusTeamAccess")

	var ta nexusv1alpha1.NexusTeamAccess
	if err := r.Get(ctx, req.NamespacedName, &ta); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения NexusTeamAccess: %w", err)
	}

	if !ta.ObjectMeta.DeletionTimestamp.IsZero() {
		if utils.ContainsString(ta.Finalizers, teamAccessFinalizer) {
			ta.Finalizers = utils.RemoveString(ta.Finalizers, teamAccessFinalizer)
			opmetrics.DeleteResourceReady("nexusteamaccess", ta.Namespace, ta.Name)
			if err := r.Update(ctx, &ta); err != nil {
				return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	if !utils.ContainsString(ta.Finalizers, teamAccessFinalizer) {
		ta.Finalizers = append(ta.Finalizers, teamAccessFinalizer)
		if err := r.Update(ctx, &ta); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка при добавлении финализатора: %w", err)
		}
	}

	return r.syncTeamAccess(ctx, &ta, log)
}

func (r *NexusTeamAccessReconciler) syncTeamAccess(
	ctx context.Context,
	ta *nexusv1alpha1.NexusTeamAccess,
	log logr.Logger,
) (ctrl.Result, error) {
	teamName := sanitizeTeamName(ta.Spec.TeamPath)

	desiredCS, desiredPriv, desiredPerRepoRoles, desiredAggRoles, err := r.buildDesiredResources(ta, teamName)
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
		return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка генерации ресурсов: %w", err))
	}

	// Create or update all child resources
	totalResources := len(desiredCS) + len(desiredPriv) + len(desiredPerRepoRoles) + len(desiredAggRoles)
	generatedResources := make([]nexusv1alpha1.GeneratedResource, 0, totalResources)

	for i := range desiredCS {
		cs := &desiredCS[i]
		if err := controllerutil.SetControllerReference(ta, cs, r.Scheme); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка установки ownerRef для ContentSelector %s: %w", cs.Name, err))
		}
		if _, err := r.createOrUpdateCS(ctx, cs); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка синхронизации ContentSelector %s: %w", cs.Name, err))
		}
		generatedResources = append(generatedResources, nexusv1alpha1.GeneratedResource{
			Kind: "ContentSelector", Name: cs.Name, NexusName: cs.Spec.Name,
		})
	}

	for i := range desiredPriv {
		priv := &desiredPriv[i]
		if err := controllerutil.SetControllerReference(ta, priv, r.Scheme); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка установки ownerRef для Privilege %s: %w", priv.Name, err))
		}
		if _, err := r.createOrUpdatePrivilege(ctx, priv); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка синхронизации Privilege %s: %w", priv.Name, err))
		}
		generatedResources = append(generatedResources, nexusv1alpha1.GeneratedResource{
			Kind: "Privilege", Name: priv.Name, NexusName: priv.Spec.Name,
		})
	}

	allRoles := append(desiredPerRepoRoles, desiredAggRoles...)
	for i := range allRoles {
		role := &allRoles[i]
		if err := controllerutil.SetControllerReference(ta, role, r.Scheme); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка установки ownerRef для Role %s: %w", role.Name, err))
		}
		if _, err := r.createOrUpdateRole(ctx, role); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusteamaccess").Inc()
			return r.updateStatus(ctx, ta, false, fmt.Errorf("ошибка синхронизации Role %s: %w", role.Name, err))
		}
		generatedResources = append(generatedResources, nexusv1alpha1.GeneratedResource{
			Kind: "Role", Name: role.Name, NexusName: role.Spec.RoleID,
		})
	}

	// Delete orphaned resources
	if err := r.deleteOrphans(ctx, ta, generatedResources, log); err != nil {
		log.Error(err, "Ошибка удаления orphan-ресурсов")
	}

	ta.Status.GeneratedResources = generatedResources
	opmetrics.SyncTotal.WithLabelValues("nexusteamaccess", "success").Inc()
	return r.updateStatus(ctx, ta, true, nil)
}

func (r *NexusTeamAccessReconciler) buildDesiredResources(
	ta *nexusv1alpha1.NexusTeamAccess,
	teamName string,
) (
	[]nexusv1alpha1.ContentSelector,
	[]nexusv1alpha1.Privilege,
	[]nexusv1alpha1.Role,
	[]nexusv1alpha1.Role,
	error,
) {
	var (
		contentSelectors []nexusv1alpha1.ContentSelector
		privileges       []nexusv1alpha1.Privilege
		perRepoRoles     []nexusv1alpha1.Role
	)

	// Per access level: collect per-repo role IDs for aggregate roles
	perRepoRoleIDs := make(map[string][]string) // accessLevel -> []roleID

	labels := map[string]string{
		labelTeamAccess: ta.Name,
		labelManagedBy:  managedByValue,
	}

	for _, repoGroup := range ta.Spec.Repositories {
		// Per-group access levels override global spec.AccessLevels
		levels := ta.Spec.AccessLevels
		if len(repoGroup.AccessLevels) > 0 {
			levels = repoGroup.AccessLevels
		}

		for _, repoName := range repoGroup.Names {
			repoSanitized := utils.SanitizeK8sName(repoName)
			csNexusName := fmt.Sprintf("cs-%s-%s-%s", teamName, repoGroup.Format, repoSanitized)
			csK8sName := utils.SanitizeK8sName(csNexusName)

			// Build CSEL expression
			expression, err := buildCSELExpression(repoGroup.Format, ta.Spec.TeamPath, repoGroup.PathPrefix)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("ошибка построения CSEL для формата %s: %w", repoGroup.Format, err)
			}

			cs := nexusv1alpha1.ContentSelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      csK8sName,
					Namespace: ta.Namespace,
					Labels:    labels,
				},
				Spec: nexusv1alpha1.ContentSelectorSpec{
					Name:        csNexusName,
					Description: fmt.Sprintf("Content selector for team %s, repo %s (%s)", ta.Spec.TeamPath, repoName, repoGroup.Format),
					Expression:  expression,
				},
			}
			contentSelectors = append(contentSelectors, cs)

			for _, level := range levels {
				actions, ok := accessLevelActions[level]
				if !ok {
					return nil, nil, nil, nil, fmt.Errorf("%w: %s", errUnknownAccessLevel, level)
				}

				privNexusName := fmt.Sprintf("%s-%s", csNexusName, level)
				privK8sName := utils.SanitizeK8sName(privNexusName)

				priv := nexusv1alpha1.Privilege{
					ObjectMeta: metav1.ObjectMeta{
						Name:      privK8sName,
						Namespace: ta.Namespace,
						Labels:    labels,
					},
					Spec: nexusv1alpha1.PrivilegeSpec{
						Name:        privNexusName,
						Type:        "repository-content-selector",
						Description: fmt.Sprintf("Privilege %s for team %s, repo %s (%s)", level, ta.Spec.TeamPath, repoName, repoGroup.Format),
						RepositoryContentSelector: &nexusv1alpha1.RepositoryContentSelectorConfig{
							Repository:      repoName,
							ContentSelector: csNexusName,
							Format:          repoGroup.Format,
							Actions:         actions,
						},
					},
				}
				privileges = append(privileges, priv)

				// Per-repo role
				roleID := fmt.Sprintf("nx-%s-%s-%s-%s", teamName, repoGroup.Format, repoSanitized, level)
				roleK8sName := utils.SanitizeK8sName(roleID)

				role := nexusv1alpha1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleK8sName,
						Namespace: ta.Namespace,
						Labels:    labels,
					},
					Spec: nexusv1alpha1.RoleSpec{
						RoleID:      roleID,
						Name:        roleID,
						Description: fmt.Sprintf("Per-repo role %s for team %s, repo %s (%s)", level, ta.Spec.TeamPath, repoName, repoGroup.Format),
						Privileges: append(
							[]string{privNexusName},
							repoViewPrivileges(repoGroup.Format, repoName, level)...,
						),
					},
				}

				// Add subrole if hierarchy exists (rw includes ro, rwd includes rw)
				if parentLevel, ok := accessLevelHierarchy[level]; ok {
					parentRoleID := fmt.Sprintf("nx-%s-%s-%s-%s", teamName, repoGroup.Format, repoSanitized, parentLevel)
					role.Spec.Roles = []string{parentRoleID}
				}

				perRepoRoles = append(perRepoRoles, role)
				perRepoRoleIDs[level] = append(perRepoRoleIDs[level], roleID)
			}
		}
	}

	// Build aggregate roles
	aggRoles := make([]nexusv1alpha1.Role, 0, len(perRepoRoleIDs))

	// Sort access levels for deterministic output
	sortedLevels := make([]string, 0, len(perRepoRoleIDs))
	for level := range perRepoRoleIDs {
		sortedLevels = append(sortedLevels, level)
	}
	sort.Strings(sortedLevels)

	for _, level := range sortedLevels {
		roleIDs := perRepoRoleIDs[level]
		aggRoleID := fmt.Sprintf("nexus-%s-%s", teamName, level)
		aggK8sName := utils.SanitizeK8sName(aggRoleID)

		subroles := make([]string, 0, len(roleIDs)+1)
		subroles = append(subroles, roleIDs...)

		// Aggregate rw includes aggregate ro, rwd includes aggregate rw
		if parentLevel, ok := accessLevelHierarchy[level]; ok {
			parentAggRoleID := fmt.Sprintf("nexus-%s-%s", teamName, parentLevel)
			subroles = append(subroles, parentAggRoleID)
		}

		aggRole := nexusv1alpha1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      aggK8sName,
				Namespace: ta.Namespace,
				Labels:    labels,
			},
			Spec: nexusv1alpha1.RoleSpec{
				RoleID:      aggRoleID,
				Name:        aggRoleID,
				Description: fmt.Sprintf("Aggregate role %s for team %s", level, ta.Spec.TeamPath),
				Roles:       subroles,
			},
		}
		aggRoles = append(aggRoles, aggRole)
	}

	return contentSelectors, privileges, perRepoRoles, aggRoles, nil
}

// buildCSELExpression generates a CSEL expression for the given format and team path.
// pathPrefix overrides the default prefix for the format (e.g. "/ru/" for maven2).
func buildCSELExpression(format, teamPath string, pathPrefix *string) (string, error) {
	// Normalize: remove leading/trailing slashes
	teamPath = strings.Trim(teamPath, "/")
	parts := strings.Split(teamPath, "/")

	switch format {
	case "docker":
		prefix := "/v2/"
		if pathPrefix != nil {
			prefix = *pathPrefix
		}
		return fmt.Sprintf(`path =~ "^%s(%s/.*)?$"`, prefix, teamPath), nil
	case "maven2":
		prefix := "/ru/"
		if pathPrefix != nil {
			prefix = *pathPrefix
		}
		return fmt.Sprintf(`path =~ "^%s(%s/.*)?$"`, prefix, teamPath), nil
	case "raw":
		prefix := "/"
		if pathPrefix != nil {
			prefix = *pathPrefix
		}
		return fmt.Sprintf(`path =~ "^%s(%s/.*)?$"`, prefix, teamPath), nil
	case "npm":
		// NPM scope: @part1.part2.part3
		scope := strings.Join(parts, ".")
		return fmt.Sprintf(`path =~ "^@%s.*$"`, scope), nil
	case "nuget":
		// NuGet package prefix: part1.part2.part3
		prefix := strings.Join(parts, ".")
		return fmt.Sprintf(`path =~ "^%s.*$"`, prefix), nil
	default:
		return "", fmt.Errorf("%w: %s", errUnsupportedFormat, format)
	}
}

// repoViewPrivileges returns built-in Nexus repository-view privilege names
// that must be added to per-repo roles for Browse UI to work.
// Content-selector privileges alone are not sufficient for Nexus Browse.
func repoViewPrivileges(format, repoName, level string) []string {
	viewPrivName := func(action string) string {
		return fmt.Sprintf("nx-repository-view-%s-%s-%s", format, repoName, action)
	}
	switch level {
	case "ro":
		return []string{viewPrivName("browse"), viewPrivName("read")}
	case "rw":
		return []string{viewPrivName("browse"), viewPrivName("read"), viewPrivName("add"), viewPrivName("edit")}
	case "rwd":
		return []string{viewPrivName("browse"), viewPrivName("read"), viewPrivName("add"), viewPrivName("edit"), viewPrivName("delete")}
	default:
		return []string{viewPrivName("browse"), viewPrivName("read")}
	}
}

// sanitizeTeamName converts team path to a K8s-safe name (slash→dash, lowercase).
func sanitizeTeamName(teamPath string) string {
	s := strings.Trim(teamPath, "/")
	s = strings.ReplaceAll(s, "/", "-")
	return utils.SanitizeK8sName(s)
}

func (r *NexusTeamAccessReconciler) createOrUpdateCS(
	ctx context.Context,
	desired *nexusv1alpha1.ContentSelector,
) (controllerutil.OperationResult, error) {
	existing := &nexusv1alpha1.ContentSelector{}
	existing.Name = desired.Name
	existing.Namespace = desired.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = desired.Labels
		existing.Spec = desired.Spec
		if len(desired.OwnerReferences) > 0 {
			existing.OwnerReferences = desired.OwnerReferences
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("createOrUpdate ContentSelector %s: %w", desired.Name, err)
	}
	return result, nil
}

func (r *NexusTeamAccessReconciler) createOrUpdatePrivilege(
	ctx context.Context,
	desired *nexusv1alpha1.Privilege,
) (controllerutil.OperationResult, error) {
	existing := &nexusv1alpha1.Privilege{}
	existing.Name = desired.Name
	existing.Namespace = desired.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = desired.Labels
		existing.Spec = desired.Spec
		if len(desired.OwnerReferences) > 0 {
			existing.OwnerReferences = desired.OwnerReferences
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("createOrUpdate Privilege %s: %w", desired.Name, err)
	}
	return result, nil
}

func (r *NexusTeamAccessReconciler) createOrUpdateRole(
	ctx context.Context,
	desired *nexusv1alpha1.Role,
) (controllerutil.OperationResult, error) {
	existing := &nexusv1alpha1.Role{}
	existing.Name = desired.Name
	existing.Namespace = desired.Namespace

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = desired.Labels
		existing.Spec = desired.Spec
		if len(desired.OwnerReferences) > 0 {
			existing.OwnerReferences = desired.OwnerReferences
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("createOrUpdate Role %s: %w", desired.Name, err)
	}
	return result, nil
}

func (r *NexusTeamAccessReconciler) deleteOrphans(
	ctx context.Context,
	ta *nexusv1alpha1.NexusTeamAccess,
	currentResources []nexusv1alpha1.GeneratedResource,
	log logr.Logger,
) error {
	currentNames := make(map[string]bool)
	for _, res := range currentResources {
		key := res.Kind + "/" + res.Name
		currentNames[key] = true
	}

	matchLabels := client.MatchingLabels{
		labelTeamAccess: ta.Name,
		labelManagedBy:  managedByValue,
	}
	inNamespace := client.InNamespace(ta.Namespace)

	// Delete orphan ContentSelectors
	var csList nexusv1alpha1.ContentSelectorList
	if err := r.List(ctx, &csList, matchLabels, inNamespace); err != nil {
		return fmt.Errorf("ошибка получения списка ContentSelector: %w", err)
	}
	for i := range csList.Items {
		if !currentNames["ContentSelector/"+csList.Items[i].Name] {
			log.Info("Удаление orphan ContentSelector", "name", csList.Items[i].Name)
			if err := r.Delete(ctx, &csList.Items[i]); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("ошибка удаления orphan ContentSelector %s: %w", csList.Items[i].Name, err)
			}
		}
	}

	// Delete orphan Privileges
	var privList nexusv1alpha1.PrivilegeList
	if err := r.List(ctx, &privList, matchLabels, inNamespace); err != nil {
		return fmt.Errorf("ошибка получения списка Privilege: %w", err)
	}
	for i := range privList.Items {
		if !currentNames["Privilege/"+privList.Items[i].Name] {
			log.Info("Удаление orphan Privilege", "name", privList.Items[i].Name)
			if err := r.Delete(ctx, &privList.Items[i]); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("ошибка удаления orphan Privilege %s: %w", privList.Items[i].Name, err)
			}
		}
	}

	// Delete orphan Roles
	var roleList nexusv1alpha1.RoleList
	if err := r.List(ctx, &roleList, matchLabels, inNamespace); err != nil {
		return fmt.Errorf("ошибка получения списка Role: %w", err)
	}
	for i := range roleList.Items {
		if !currentNames["Role/"+roleList.Items[i].Name] {
			log.Info("Удаление orphan Role", "name", roleList.Items[i].Name)
			if err := r.Delete(ctx, &roleList.Items[i]); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("ошибка удаления orphan Role %s: %w", roleList.Items[i].Name, err)
			}
		}
	}

	return nil
}

func (r *NexusTeamAccessReconciler) updateStatus(
	ctx context.Context,
	ta *nexusv1alpha1.NexusTeamAccess,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("nexusteamaccess", ta.Namespace, ta.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: ta.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "NexusTeamAccess успешно синхронизирован"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(ta.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == ta.Generation {
		if ready {
			return ctrl.Result{RequeueAfter: teamAccessResyncPeriod}, nil
		}
		return ctrl.Result{RequeueAfter: teamAccessRequeueDelay}, nil
	}

	meta.SetStatusCondition(&ta.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		ta.Status.LastSyncTime = &now
		ta.Status.SyncErrors = 0
	} else {
		ta.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, ta); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{RequeueAfter: teamAccessResyncPeriod}, nil
	}
	return ctrl.Result{RequeueAfter: teamAccessRequeueDelay}, nil
}

func (r *NexusTeamAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ownsPred := builder.WithPredicates(predicate.GenerationChangedPredicate{})

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.NexusTeamAccess{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&nexusv1alpha1.ContentSelector{}, ownsPred).
		Owns(&nexusv1alpha1.Privilege{}, ownsPred).
		Owns(&nexusv1alpha1.Role{}, ownsPred).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
