package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/keycloak"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	bindingFinalizer    = "nexus.kostoed.ru/teambinding"
	bindingRequeueDelay = 30 * time.Second
	bindingResyncPeriod = 5 * time.Minute
)

// NexusTeamBindingReconciler reconciles NexusTeamBinding objects.
type NexusTeamBindingReconciler struct {
	client.Client
	ExternalClients
	Log logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteambindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteambindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteambindings/finalizers,verbs=update
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusteamaccesses,verbs=get;list;watch

func (r *NexusTeamBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("nexusteambinding", req.NamespacedName)
	log.V(1).Info("Reconcile NexusTeamBinding")

	var binding nexusv1alpha1.NexusTeamBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get NexusTeamBinding: %w", err)
	}

	kcClient, err := r.keycloakClient()
	if err != nil {
		log.Error(err, "Keycloak client unavailable")
		return r.updateStatus(ctx, &binding, false, err)
	}

	// Handle deletion.
	if !binding.ObjectMeta.DeletionTimestamp.IsZero() {
		if utils.ContainsString(binding.Finalizers, bindingFinalizer) {
			if err := r.cleanupBindings(ctx, &binding, kcClient, log); err != nil {
				log.Error(err, "Failed to cleanup role bindings")
			}
			binding.Finalizers = utils.RemoveString(binding.Finalizers, bindingFinalizer)
			opmetrics.DeleteResourceReady("nexusteambinding", binding.Namespace, binding.Name)
			if err := r.Update(ctx, &binding); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer.
	if !utils.ContainsString(binding.Finalizers, bindingFinalizer) {
		binding.Finalizers = append(binding.Finalizers, bindingFinalizer)
		if err := r.Update(ctx, &binding); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	// Resolve referenced NexusTeamAccess.
	var ta nexusv1alpha1.NexusTeamAccess
	if err := r.Get(ctx, types.NamespacedName{
		Name:      binding.Spec.TeamAccessRef,
		Namespace: binding.Namespace,
	}, &ta); err != nil {
		if k8serrors.IsNotFound(err) {
			return r.updateStatus(ctx, &binding, false,
				fmt.Errorf("%w: %s", errNTANotFound, binding.Spec.TeamAccessRef))
		}
		return ctrl.Result{}, fmt.Errorf("get NexusTeamAccess: %w", err)
	}

	// Build team name from NTA.
	teamName := sanitizeTeamName(ta.Spec.TeamPath)

	// Collect available aggregate role names.
	availableRoles := make(map[string]bool)
	for _, res := range ta.Status.GeneratedResources {
		if res.Kind == "Role" && strings.HasPrefix(res.NexusName, "nexus-") {
			availableRoles[res.NexusName] = true
		}
	}

	// Sync each member.
	var syncErrors int
	for _, member := range binding.Spec.Members {
		roleName := fmt.Sprintf("nexus-%s-%s", teamName, member.AccessLevel)

		if !availableRoles[roleName] {
			log.Error(nil, "Aggregate role not found in NTA status",
				"role", roleName, "username", member.Username)
			syncErrors++
			continue
		}

		if err := r.syncMember(ctx, kcClient, member, roleName, teamName, log); err != nil {
			log.Error(err, "Failed to sync member", "username", member.Username, "role", roleName)
			syncErrors++
		}
	}

	if syncErrors > 0 {
		return r.updateStatus(ctx, &binding, false,
			fmt.Errorf("%w: %d members", errMemberSyncFailed, syncErrors))
	}

	return r.updateStatus(ctx, &binding, true, nil)
}

// syncMember ensures a user has the correct Keycloak client role for this team.
func (r *NexusTeamBindingReconciler) syncMember(
	ctx context.Context,
	kcClient *keycloak.Client,
	member nexusv1alpha1.TeamMember,
	targetRoleName, teamName string,
	log logr.Logger,
) error {
	// Find user in Keycloak.
	user, err := kcClient.GetUserByUsername(ctx, member.Username)
	if err != nil {
		return fmt.Errorf("find user %s: %w", member.Username, err)
	}

	// Get the target role (need ID for assignment).
	targetRole, err := kcClient.GetClientRoleByName(ctx, targetRoleName)
	if err != nil {
		return fmt.Errorf("get role %s: %w", targetRoleName, err)
	}

	// Get current client roles for this user.
	currentRoles, err := kcClient.GetUserClientRoles(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("get user roles: %w", err)
	}

	// Check if user already has the target role.
	teamPrefix := "nexus-" + teamName + "-"
	hasTarget := false
	var rolesToRemove []keycloak.Role

	for _, r := range currentRoles {
		if r.Name == targetRoleName {
			hasTarget = true
			continue
		}
		// Remove other roles for the same team (e.g., user had rw, now ro).
		if strings.HasPrefix(r.Name, teamPrefix) {
			rolesToRemove = append(rolesToRemove, r)
		}
	}

	// Remove old team roles.
	if len(rolesToRemove) > 0 {
		if err := kcClient.UnassignClientRoles(ctx, user.ID, rolesToRemove); err != nil {
			return fmt.Errorf("unassign old roles: %w", err)
		}
		for _, r := range rolesToRemove {
			log.Info("Removed old role", "username", member.Username, "role", r.Name)
		}
	}

	// Assign target role if not present.
	if !hasTarget {
		if err := kcClient.AssignClientRoles(ctx, user.ID, []keycloak.Role{*targetRole}); err != nil {
			return fmt.Errorf("assign role %s: %w", targetRoleName, err)
		}
		log.Info("Assigned role", "username", member.Username, "role", targetRoleName)
	}

	return nil
}

// cleanupBindings removes all Keycloak role assignments managed by this binding.
func (r *NexusTeamBindingReconciler) cleanupBindings(
	ctx context.Context,
	binding *nexusv1alpha1.NexusTeamBinding,
	kcClient *keycloak.Client,
	log logr.Logger,
) error {
	// Resolve NTA for team name.
	var ta nexusv1alpha1.NexusTeamAccess
	if err := r.Get(ctx, types.NamespacedName{
		Name:      binding.Spec.TeamAccessRef,
		Namespace: binding.Namespace,
	}, &ta); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Referenced NTA already deleted, skipping role cleanup")
			return nil
		}
		return fmt.Errorf("get NTA for cleanup: %w", err)
	}

	teamName := sanitizeTeamName(ta.Spec.TeamPath)

	var errs []error
	for _, member := range binding.Spec.Members {
		roleName := fmt.Sprintf("nexus-%s-%s", teamName, member.AccessLevel)

		user, err := kcClient.GetUserByUsername(ctx, member.Username)
		if err != nil {
			log.V(1).Info("User not found during cleanup, skipping", "username", member.Username)
			continue
		}

		role, err := kcClient.GetClientRoleByName(ctx, roleName)
		if err != nil {
			log.V(1).Info("Role not found during cleanup, skipping", "role", roleName)
			continue
		}

		if err := kcClient.UnassignClientRoles(ctx, user.ID, []keycloak.Role{*role}); err != nil {
			log.Error(err, "Failed to unassign role during cleanup", "username", member.Username, "role", roleName)
			errs = append(errs, err)
		} else {
			log.Info("Unassigned role during cleanup", "username", member.Username, "role", roleName)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %d bindings", errBindingCleanupFailed, len(errs))
	}
	return nil
}

func (r *NexusTeamBindingReconciler) updateStatus(
	ctx context.Context,
	binding *nexusv1alpha1.NexusTeamBinding,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("nexusteambinding", binding.Namespace, binding.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: binding.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = "Success"
		newCondition.Message = "All members synced to Keycloak"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = "SyncError"
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(binding.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == binding.Generation {
		if ready {
			return ctrl.Result{RequeueAfter: bindingResyncPeriod}, nil
		}
		return ctrl.Result{RequeueAfter: bindingRequeueDelay}, nil
	}

	meta.SetStatusCondition(&binding.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		binding.Status.LastSyncTime = &now
		binding.Status.SyncErrors = 0
	} else {
		binding.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, binding); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	if ready {
		return ctrl.Result{RequeueAfter: bindingResyncPeriod}, nil
	}
	return ctrl.Result{RequeueAfter: bindingRequeueDelay}, nil
}

func (r *NexusTeamBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.NexusTeamBinding{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create NexusTeamBinding controller: %w", err)
	}
	return nil
}
