//
// Copyright (c) 2019-2021 Red Hat, Inc.
// This program and the accompanying materials are made
// available under the terms of the Eclipse Public License 2.0
// which is available at https://www.eclipse.org/legal/epl-2.0/
//
// SPDX-License-Identifier: EPL-2.0
//
// Contributors:
//   Red Hat, Inc. - initial API and implementation
//

package main

import (
	"context"
	"flag"
	"os"

	"github.com/devfile/devworkspace-operator/controllers/controller/workspacerouting"
	"github.com/devfile/devworkspace-operator/controllers/controller/workspacerouting/solvers"
	"github.com/devfile/devworkspace-operator/internal/cluster"
	"github.com/devfile/devworkspace-operator/pkg/config"
	"github.com/devfile/devworkspace-operator/pkg/webhook"

	workspacev1alpha1 "github.com/devfile/api/v2/pkg/apis/workspaces/v1alpha1"
	workspacev1alpha2 "github.com/devfile/api/v2/pkg/apis/workspaces/v1alpha2"
	controllerv1alpha1 "github.com/devfile/devworkspace-operator/apis/controller/v1alpha1"
	workspacecontroller "github.com/devfile/devworkspace-operator/controllers/workspace"

	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(controllerv1alpha1.AddToScheme(scheme))
	utilruntime.Must(workspacev1alpha1.AddToScheme(scheme))
	utilruntime.Must(workspacev1alpha2.AddToScheme(scheme))

	if isOS, err := cluster.IsOpenShift(); isOS && err == nil {
		utilruntime.Must(routev1.Install(scheme))
		utilruntime.Must(templatev1.Install(scheme))
		utilruntime.Must(oauthv1.Install(scheme))
	}

	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(config.GetDevModeEnabled())))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "8d217f93.devfile.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err = setupControllerConfig(mgr); err != nil {
		setupLog.Error(err, "unable to read controller configuration")
		os.Exit(1)
	}

	if err = (&workspacerouting.WorkspaceRoutingReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("WorkspaceRouting"),
		Scheme:       mgr.GetScheme(),
		SolverGetter: &solvers.SolverGetter{},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkspaceRouting")
		os.Exit(1)
	}
	if err = (&workspacecontroller.DevWorkspaceReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("DevWorkspace"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DevWorkspace")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// Get a config to talk to the apiserver
	cfg, err := ctrlconfig.GetConfig()
	if err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}

	setupLog.Info("setting up webhooks")
	if err := webhook.SetupWebhooks(context.Background(), cfg); err != nil {
		setupLog.Error(err, "failed to setup webhooks")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupControllerConfig(mgr ctrl.Manager) error {
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err == nil {
		config.ConfigMapReference.Namespace = operatorNamespace
	} else {
		config.ConfigMapReference.Namespace = os.Getenv(cluster.WatchNamespaceEnvVar)
	}
	err = config.WatchControllerConfig(mgr)
	if err != nil {
		return err
	}

	// Check if we're running on OpenShift
	isOS, err := cluster.IsOpenShift()
	if err != nil {
		return err
	}
	config.ControllerCfg.SetIsOpenShift(isOS)

	err = config.ControllerCfg.Validate()
	if err != nil {
		setupLog.Error(err, "Controller configuration is invalid")
		return err
	}
	return nil
}
