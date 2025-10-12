/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mirrorv1alpha1 "github.com/sarataha/configmirror-operator/api/v1alpha1"
	"github.com/sarataha/configmirror-operator/internal/database"
)

const (
	finalizerName = "mirror.pawapay.io/finalizer"
	ownerLabel    = "mirror.pawapay.io/owner"
)

// ConfigMirrorReconciler reconciles a ConfigMirror object
type ConfigMirrorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	DBClient *database.Client
}

// +kubebuilder:rbac:groups=mirror.pawapay.io,resources=configmirrors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mirror.pawapay.io,resources=configmirrors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mirror.pawapay.io,resources=configmirrors/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func (r *ConfigMirrorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	configMirror := &mirrorv1alpha1.ConfigMirror{}
	if err := r.Get(ctx, req.NamespacedName, configMirror); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ConfigMirror")
		return ctrl.Result{}, err
	}

	if configMirror.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(configMirror, finalizerName) {
			controllerutil.AddFinalizer(configMirror, finalizerName)
			if err := r.Update(ctx, configMirror); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(configMirror, finalizerName) {
			if err := r.cleanupConfigMaps(ctx, configMirror); err != nil {
				logger.Error(err, "Failed to cleanup ConfigMaps")
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(configMirror, finalizerName)
			if err := r.Update(ctx, configMirror); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(configMirror.Spec.Selector)
	if err != nil {
		logger.Error(err, "Invalid label selector")
		r.updateStatus(ctx, configMirror, metav1.ConditionFalse, "InvalidSelector", err.Error())
		return ctrl.Result{}, err
	}

	configMapList := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMapList,
		client.InNamespace(configMirror.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		logger.Error(err, "Failed to list ConfigMaps")
		r.updateStatus(ctx, configMirror, metav1.ConditionFalse, "ListFailed", err.Error())
		return ctrl.Result{}, err
	}

	var replicatedCMs []mirrorv1alpha1.ReplicatedConfigMap
	now := metav1.Now()

	for _, cm := range configMapList.Items {
		targets := []string{}
		for _, targetNS := range configMirror.Spec.TargetNamespaces {
			if err := r.replicateConfigMap(ctx, &cm, targetNS, configMirror); err != nil {
				logger.Error(err, "Failed to replicate ConfigMap", "configmap", cm.Name, "target", targetNS)
				continue
			}
			targets = append(targets, targetNS)
		}

		if r.DBClient != nil && configMirror.Spec.Database != nil && configMirror.Spec.Database.Enabled {
			if err := r.DBClient.SaveConfigMap(ctx, &cm, configMirror.Name, configMirror.Namespace); err != nil {
				logger.Error(err, "Failed to save ConfigMap to database", "configmap", cm.Name)
			}
		}

		replicatedCMs = append(replicatedCMs, mirrorv1alpha1.ReplicatedConfigMap{
			Name:            cm.Name,
			SourceNamespace: cm.Namespace,
			Targets:         targets,
			LastSyncTime:    &now,
		})
	}

	// Cleanup orphaned replicas: find replicas that no longer have a source ConfigMap
	currentConfigMapNames := make(map[string]bool)
	for _, cm := range configMapList.Items {
		currentConfigMapNames[cm.Name] = true
	}

	// Get previously tracked ConfigMaps from status
	for _, prevCM := range configMirror.Status.ReplicatedConfigMaps {
		// If this ConfigMap no longer exists in source, delete it from targets
		if !currentConfigMapNames[prevCM.Name] {
			logger.Info("Cleaning up orphaned replicated ConfigMap", "configmap", prevCM.Name)
			for _, targetNS := range configMirror.Spec.TargetNamespaces {
				if err := r.deleteReplicatedConfigMap(ctx, prevCM.Name, targetNS, configMirror); err != nil {
					logger.Error(err, "Failed to delete orphaned ConfigMap", "configmap", prevCM.Name, "target", targetNS)
				}
			}

			// Also delete from database
			if r.DBClient != nil && configMirror.Spec.Database != nil && configMirror.Spec.Database.Enabled {
				if err := r.DBClient.DeleteConfigMap(ctx, prevCM.Name, prevCM.SourceNamespace, configMirror.Name, configMirror.Namespace); err != nil {
					logger.Error(err, "Failed to delete ConfigMap from database", "configmap", prevCM.Name)
				}
			}
		}
	}

	configMirror.Status.ReplicatedConfigMaps = replicatedCMs
	configMirror.Status.ObservedGeneration = configMirror.Generation

	if r.DBClient != nil && configMirror.Spec.Database != nil && configMirror.Spec.Database.Enabled {
		if err := r.DBClient.Ping(ctx); err == nil {
			configMirror.Status.DatabaseStatus = &mirrorv1alpha1.DatabaseStatus{
				Connected:    true,
				LastSyncTime: &now,
				Message:      "Connected",
			}
		} else {
			configMirror.Status.DatabaseStatus = &mirrorv1alpha1.DatabaseStatus{
				Connected: false,
				Message:   err.Error(),
			}
		}
	}

	r.updateStatus(ctx, configMirror, metav1.ConditionTrue, "ReconcileSuccess", "Successfully replicated ConfigMaps")

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ConfigMirrorReconciler) replicateConfigMap(ctx context.Context, source *corev1.ConfigMap, targetNS string, owner *mirrorv1alpha1.ConfigMirror) error {
	target := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      source.Name,
			Namespace: targetNS,
			Labels: map[string]string{
				ownerLabel: fmt.Sprintf("%s.%s", owner.Namespace, owner.Name),
			},
		},
		Data:       source.Data,
		BinaryData: source.BinaryData,
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, target)
		}
		return err
	}

	existing.Data = target.Data
	existing.BinaryData = target.BinaryData
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	existing.Labels[ownerLabel] = target.Labels[ownerLabel]

	return r.Update(ctx, existing)
}

func (r *ConfigMirrorReconciler) deleteReplicatedConfigMap(ctx context.Context, name, targetNS string, owner *mirrorv1alpha1.ConfigMirror) error {
	ownerLabelValue := fmt.Sprintf("%s.%s", owner.Namespace, owner.Name)

	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: targetNS}, configMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil // Already deleted, no error
		}
		return err
	}

	// Only delete if it has our owner label (safety check)
	if configMap.Labels != nil && configMap.Labels[ownerLabel] == ownerLabelValue {
		return r.Delete(ctx, configMap)
	}

	return nil // Not ours, don't delete
}

func (r *ConfigMirrorReconciler) cleanupConfigMaps(ctx context.Context, configMirror *mirrorv1alpha1.ConfigMirror) error {
	ownerLabelValue := fmt.Sprintf("%s.%s", configMirror.Namespace, configMirror.Name)

	for _, targetNS := range configMirror.Spec.TargetNamespaces {
		configMapList := &corev1.ConfigMapList{}
		if err := r.List(ctx, configMapList,
			client.InNamespace(targetNS),
			client.MatchingLabels{ownerLabel: ownerLabelValue},
		); err != nil {
			return err
		}

		for _, cm := range configMapList.Items {
			if err := r.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

func (r *ConfigMirrorReconciler) updateStatus(ctx context.Context, configMirror *mirrorv1alpha1.ConfigMirror, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             status,
		ObservedGeneration: configMirror.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	meta.SetStatusCondition(&configMirror.Status.Conditions, condition)
	_ = r.Status().Update(ctx, configMirror)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMirrorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mirrorv1alpha1.ConfigMirror{}).
		Owns(&corev1.ConfigMap{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findConfigMirrorsForConfigMap),
		).
		Named("configmirror").
		Complete(r)
}

func (r *ConfigMirrorReconciler) findConfigMirrorsForConfigMap(ctx context.Context, cm client.Object) []reconcile.Request {
	configMapObj := cm.(*corev1.ConfigMap)

	configMirrorList := &mirrorv1alpha1.ConfigMirrorList{}
	if err := r.List(ctx, configMirrorList, client.InNamespace(configMapObj.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, configMirror := range configMirrorList.Items {
		selector, err := metav1.LabelSelectorAsSelector(configMirror.Spec.Selector)
		if err != nil {
			continue
		}

		if selector.Matches(labels.Set(configMapObj.Labels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirror.Name,
					Namespace: configMirror.Namespace,
				},
			})
		}
	}

	return requests
}
