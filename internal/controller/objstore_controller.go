/*
Copyright 2026.

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
	"os"

	log "github.com/labstack/gommon/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	//v1 "k8s.io/client-go/applyconfigurations/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cninfv1alpha1 "test-operator.io/cninf/api/v1alpha1"
)

const (
	configMapName = "%s-cm"
	finalizer     = "objstores.cninf.test-operator/finalizer"
)

// ObjStoreReconciler reconciles a ObjStore object
type ObjStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	//S3svc *s3.s3
}

// +kubebuilder:rbac:groups=cninf.my.domain,resources=objstores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cninf.my.domain,resources=objstores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cninf.my.domain,resources=objstores/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;delete;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ObjStore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *ObjStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)
	instance := &cninfv1alpha1.ObjStore{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		if instance.Status.State == "" {
			instance.Status.State = cninfv1alpha1.PENDING_STATE
			r.Status().Update(ctx, instance)
		}
		controllerutil.AddFinalizer(instance, finalizer)
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
		if instance.Status.State == cninfv1alpha1.PENDING_STATE {
			log.Info("starting to create resources")
			if err := r.createResources(ctx, instance); err != nil {
				instance.Status.State = cninfv1alpha1.ERROR_STATE
				r.Status().Update(ctx, instance)
				log.Error("Error creating folder", err)
				return ctrl.Result{}, err
			}

		}

	} else {
		// deletion time has value - garbage collection here
		log.Info("In deletion flow")
		if err := r.deleteResources(ctx, instance); err != nil {
			instance.Status.State = cninfv1alpha1.ERROR_STATE
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(instance, finalizer)
		log.Info("Removing Finalizer so it gets deleted")
		if err := r.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObjStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cninfv1alpha1.ObjStore{}).
		Named("objstore").
		Complete(r)
}

func (r *ObjStoreReconciler) createResources(ctx context.Context, objStore *cninfv1alpha1.ObjStore) error {
	objStore.Status.State = cninfv1alpha1.CREATING_STATE
	err := r.Status().Update(ctx, objStore)
	if err != nil {
		return err
	}
	// create the aws bucket, but we need to create local folder here
	err = os.Mkdir(objStore.Spec.Name, 0777)
	if err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return err
	}

	// now create config map
	data := make(map[string]string, 0)
	data["folderName"] = objStore.Spec.Name
	//data["location"]  // We are not considering this parameter in our config map
	configmap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(configMapName, objStore.Name),
			Namespace: objStore.Namespace,
		},
		Data: data,
	}

	err = r.Create(ctx, configmap)
	if err != nil {
		return err
	}
	// Changing status to Created once folder is created
	objStore.Status.State = cninfv1alpha1.CREATED_STATE
	err = r.Status().Update(ctx, objStore)
	if err != nil {
		return err
	}
	return nil
}

func (r *ObjStoreReconciler) deleteResources(ctx context.Context, objStore *cninfv1alpha1.ObjStore) error {

	err := os.RemoveAll(objStore.Spec.Name)
	if err != nil {
		log.Fatalf("Error removing folder: %v", err)
	}
	configmap := &v1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKey{
		Name:      fmt.Sprintf(configMapName, objStore.Name),
		Namespace: objStore.Namespace,
	}, configmap)
	if err != nil {
		return err
	}

	err = r.Delete(ctx, configmap)
	if err != nil {
		return err
	}

	return nil
}
