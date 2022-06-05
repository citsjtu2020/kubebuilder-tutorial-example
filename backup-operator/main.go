/*


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

package main

import (
	"flag"
	"os"
	"path/filepath"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	elasticscalecomsjtucitv1 "backup-operator/api/v1"
	elasticscalev1 "backup-operator/api/v1"
	"backup-operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = elasticscalev1.AddToScheme(scheme)
	_ = elasticscalecomsjtucitv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	//init a clientset
	var kubeConfigBackCRD *string
	if home := homedir.HomeDir(); home != "" {
		kubeConfigBackCRD = flag.String("kubeconfig111", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeConfigBackCRD = flag.String("kubeconfig111", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	// SetLogger sets a concrete logging implementation for all deferred Loggers.
	// UseDevMode sets the logger to use (or not use) development mode (more human-readable output,
	// extra stack traces and logging information, etc). See Options.Development
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// NewManager returns a new Manager for creating Controllers
	// manager.New  func New(config *rest.Config, options Options) (Manager, error)
	// GetConfigOrDie creates a *rest.Config for talking to a Kubernetes apiserver.
	// If --kubeconfig is set, will use the kubeconfig file at that location.  Otherwise will assume running
	// in cluster and use the cluster provided kubeconfig.
	//
	// Will log an error and exit if there is an error creating the rest.Config.
	// Options are the arguments for creating a new Manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "c896be06.com.sjtu.cit",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	// 创建clientset客户端
	config, err := clientcmd.BuildConfigFromFlags("", *kubeConfigBackCRD)
	if err != nil {
		setupLog.Error(err, "unable to start kubeconfig")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		setupLog.Error(err, "unable to create clientSet")
	}

	// 创建BackupDeploymentReconciler
	if err = (&controllers.BackupDeploymentReconciler{
		Client:     mgr.GetClient(),
		Clientset:  *clientset,
		Log:        ctrl.Log.WithName("controllers").WithName("BackupDeployment"),
		Scheme:     mgr.GetScheme(),
		StatusLock: new(sync.RWMutex),
		Eventer:    mgr.GetEventRecorderFor("backupdeployment-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BackupDeployment")
		os.Exit(1)
	}
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err = (&elasticscalecomsjtucitv1.BackupDeployment{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "BackupDeployment")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	// Start starts all registered Controllers and blocks until the context is cancelled.
	// Returns an error if there is an error starting any controller.
	//
	// If LeaderElection is used, the binary must be exited immediately after this returns,
	// otherwise components that need leader election might continue to run after the leader
	// lock was lost.

	// SetupSignalHandler registered for SIGTERM and SIGINT. A stop channel is returned
	// which is closed on one of these signals. If a second signal is caught, the program
	// is terminated with exit code 1.
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
