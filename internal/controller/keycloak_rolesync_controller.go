package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/keycloak"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	keycloakSyncFinalizer    = "nexus.kostoed.ru/keycloak-rolesync"
	keycloakSyncRequeueDelay = 30 * time.Second
	keycloakResyncPeriod     = 10 * time.Minute
	keycloakSyncedCondition  = "KeycloakSynced"
)

// KeycloakRoleSyncReconciler watches NexusTeamAccess and syncs aggregate roles to Keycloak client roles.
type KeycloakRoleSyncReconciler struct {
	client.Client
	ExternalClients
	Log logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses/finalizers,verbs=update

func (r *KeycloakRoleSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("keycloak-rolesync", req.NamespacedName)

	var ta nexusv1alpha1.NexusTeamAccess
	if err := r.Get(ctx, req.NamespacedName, &ta); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get NexusTeamAccess: %w", err)
	}

	kcClient, err := r.keycloakClient()
	if err != nil {
		log.Error(err, "Keycloak client unavailable")
		return ctrl.Result{RequeueAfter: keycloakSyncRequeueDelay}, nil
	}

	// Handle deletion.
	if !ta.ObjectMeta.DeletionTimestamp.IsZero() {
		if utils.ContainsString(ta.Finalizers, keycloakSyncFinalizer) {
			if err := r.cleanupKeycloakRoles(ctx, &ta, kcClient, log); err != nil {
				log.Error(err, "Failed to cleanup Keycloak roles")
				return ctrl.Result{RequeueAfter: keycloakSyncRequeueDelay}, nil
			}
			ta.Finalizers = utils.RemoveString(ta.Finalizers, keycloakSyncFinalizer)
			if err := r.Update(ctx, &ta); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !utils.ContainsString(ta.Finalizers, keycloakSyncFinalizer) {
		ta.Finalizers = append(ta.Finalizers, keycloakSyncFinalizer)
		if err := r.Update(ctx, &ta); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	// Wait until NTA is Ready (aggregate roles exist in status).
	readyCond := meta.FindStatusCondition(ta.Status.Conditions, "Ready")
	if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
		log.V(1).Info("NexusTeamAccess not ready yet, requeue")
		return ctrl.Result{RequeueAfter: keycloakSyncRequeueDelay}, nil
	}

	// Extract aggregate role names from generated resources.
	aggRoleNames := extractAggregateRoleNames(&ta)
	if len(aggRoleNames) == 0 {
		log.V(1).Info("No aggregate roles found in status")
		return ctrl.Result{RequeueAfter: keycloakResyncPeriod}, nil
	}

	// Dedup: skip sync if the set of roles hasn't changed.
	rolesHash := computeRolesHash(aggRoleNames)
	syncedCond := meta.FindStatusCondition(ta.Status.Conditions, keycloakSyncedCondition)
	if syncedCond != nil &&
		syncedCond.Status == metav1.ConditionTrue &&
		syncedCond.Message == rolesHash {
		log.V(1).Info("Keycloak roles already synced, skipping", "hash", rolesHash)
		return ctrl.Result{RequeueAfter: keycloakResyncPeriod}, nil
	}

	// Ensure each aggregate role exists as a Keycloak client role.
	var syncErrors int
	for _, roleName := range aggRoleNames {
		if err := kcClient.EnsureClientRole(ctx, roleName, ""); err != nil {
			log.Error(err, "Failed to ensure Keycloak client role", "role", roleName)
			syncErrors++
		} else {
			log.V(1).Info("Keycloak client role ensured", "role", roleName)
		}
	}

	if syncErrors > 0 {
		return ctrl.Result{RequeueAfter: keycloakSyncRequeueDelay}, nil
	}

	// Update KeycloakSynced condition with roles hash.
	meta.SetStatusCondition(&ta.Status.Conditions, metav1.Condition{
		Type:               keycloakSyncedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            rolesHash,
		ObservedGeneration: ta.Generation,
	})
	if err := r.Status().Update(ctx, &ta); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update KeycloakSynced status: %w", err)
	}

	log.Info("Keycloak roles synced", "count", len(aggRoleNames))
	return ctrl.Result{RequeueAfter: keycloakResyncPeriod}, nil
}

// extractAggregateRoleNames returns aggregate role nexusNames from NTA status.
// Aggregate roles have naming pattern "nexus-{teamName}-{level}".
func extractAggregateRoleNames(ta *nexusv1alpha1.NexusTeamAccess) []string {
	var names []string
	for _, res := range ta.Status.GeneratedResources {
		if res.Kind == "Role" && strings.HasPrefix(res.NexusName, "nexus-") {
			names = append(names, res.NexusName)
		}
	}
	return names
}

// computeRolesHash returns a stable hash of sorted role names for dedup.
func computeRolesHash(roleNames []string) string {
	sorted := make([]string, len(roleNames))
	copy(sorted, roleNames)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return fmt.Sprintf("%x", h[:8])
}

// cleanupKeycloakRoles removes Keycloak client roles for this NTA.
func (r *KeycloakRoleSyncReconciler) cleanupKeycloakRoles(
	ctx context.Context,
	ta *nexusv1alpha1.NexusTeamAccess,
	kcClient *keycloak.Client,
	log logr.Logger,
) error {
	roleNames := extractAggregateRoleNames(ta)
	var errs []error
	for _, roleName := range roleNames {
		if err := kcClient.DeleteClientRole(ctx, roleName); err != nil {
			log.Error(err, "Failed to delete Keycloak client role", "role", roleName)
			errs = append(errs, err)
		} else {
			log.Info("Deleted Keycloak client role", "role", roleName)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %d roles", errKeycloakCleanupFailed, len(errs))
	}
	return nil
}

func (r *KeycloakRoleSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.NexusTeamAccess{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create KeycloakRoleSync controller: %w", err)
	}
	return nil
}
