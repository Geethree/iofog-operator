package kog

import (
	"context"
	"strconv"
	"strings"

	iokogv1alpha1 "github.com/eclipse-iofog/iofog-operator/pkg/apis/iokog/v1alpha1"
	"github.com/eclipse-iofog/iofog-operator/pkg/controller/kog/install"

	iofogclient "github.com/eclipse-iofog/iofogctl/pkg/iofog/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	//"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_kog")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Kog Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileKog{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("kog-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Kog
	err = c.Watch(&source.Kind{Type: &iokogv1alpha1.Kog{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Kog
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &iokogv1alpha1.Kog{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileKog implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileKog{}

// ReconcileKog reconciles a Kog object
type ReconcileKog struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Kog object and makes changes based on the state read
// and what is in the Kog.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileKog) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Kog")

	// Fetch the Kog instance
	instance := &iokogv1alpha1.Kog{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	install.SetVerbosity(true)
	installer, err := install.NewKubernetes("default")
	if err != nil {
		return reconcile.Result{}, err
	}

	if instance.Spec.ControllerCount > 0 {
		dep, svc, err := installer.CreateController(instance.Spec.ControllerCount)
		if err != nil {
			return reconcile.Result{}, err
		}
		if err := controllerutil.SetControllerReference(instance, dep, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
		if err := controllerutil.SetControllerReference(instance, svc, r.scheme); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Create new user
	endpoint, err := installer.GetControllerEndpoint()
	if err != nil {
		return reconcile.Result{}, err
	}
	ctrlClient := iofogclient.New(endpoint)
	if err = ctrlClient.CreateUser(iofogclient.User(instance.Spec.IofogUser)); err != nil {
		// If not error about account existing, fail
		if !strings.Contains(err.Error(), "already an account associated") {
			return reconcile.Result{}, err
		}
		// Try to log in
		if err = ctrlClient.Login(iofogclient.LoginRequest{
			Email:    instance.Spec.IofogUser.Email,
			Password: instance.Spec.IofogUser.Password,
		}); err != nil {
			return reconcile.Result{}, err
		}
	}

	dep, svcAcc, roleBinding, err := installer.CreateExtensionServices(install.IofogUser(instance.Spec.IofogUser))
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := controllerutil.SetControllerReference(instance, dep, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	if err := controllerutil.SetControllerReference(instance, svcAcc, r.scheme); err != nil {
		return reconcile.Result{}, err
	}
	if err := controllerutil.SetControllerReference(instance, roleBinding, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	if instance.Spec.ConnectorCount > 0 {
		controllerEndpoint, err := installer.GetControllerEndpoint()
		if err != nil {
			return reconcile.Result{}, err
		}
		for i := 0; i < instance.Spec.ConnectorCount; i++ {
			dep, svc, err := installer.CreateConnector("connector-"+strconv.Itoa(i), controllerEndpoint, install.IofogUser(instance.Spec.IofogUser))
			if err != nil {
				return reconcile.Result{}, err
			}
			if err := controllerutil.SetControllerReference(instance, dep, r.scheme); err != nil {
				return reconcile.Result{}, err
			}
			if err := controllerutil.SetControllerReference(instance, svc, r.scheme); err != nil {
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil

	//// Set Kog instance as the owner and controller
	//if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
	//	return reconcile.Result{}, err
	//}

	//// Check if this Pod already exists
	//found := &corev1.Pod{}
	//err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	//if err != nil && errors.IsNotFound(err) {
	//	reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
	//	err = r.client.Create(context.TODO(), pod)
	//	if err != nil {
	//		return reconcile.Result{}, err
	//	}

	//	// Pod created successfully - don't requeue
	//	return reconcile.Result{}, nil
	//} else if err != nil {
	//	return reconcile.Result{}, err
	//}

	//// Pod already exists - don't requeue
	//reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	//return reconcile.Result{}, nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *iokogv1alpha1.Kog) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-pod",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}
