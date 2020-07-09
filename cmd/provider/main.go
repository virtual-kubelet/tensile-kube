/*
 * Copyright ©2020. The virtual-kubelet authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	cli "github.com/virtual-kubelet/node-cli"
	logruscli "github.com/virtual-kubelet/node-cli/logrus"
	"github.com/virtual-kubelet/node-cli/opts"
	"github.com/virtual-kubelet/node-cli/provider"
	"github.com/virtual-kubelet/tensile-kube/pkg/util"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"
	"golang.org/x/time/rate"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/workqueue"

	"github.com/virtual-kubelet/tensile-kube/pkg/controllers"
	k8sprovider "github.com/virtual-kubelet/tensile-kube/pkg/provider"
)

var (
	buildVersion         = "N/A"
	buildTime            = "N/A"
	k8sVersion           = "v1.14.3"
	numberOfWorkers      = 50
	ignoreLabels         = ""
	disableRBAC          = false
	enableControllers    = ""
	enableServiceAccount = true
)

func main() {
	var cc k8sprovider.ClientConfig
	ctx := cli.ContextWithCancelOnSignal(context.Background())
	flags := pflag.NewFlagSet("client", pflag.ContinueOnError)
	flags.IntVar(&cc.KubeClientBurst, "client-burst", 1000, "qpi burst for client cluster.")
	flags.IntVar(&cc.KubeClientQPS, "client-qps", 500, "qpi qps for client cluster.")
	flags.StringVar(&cc.ClientKubeConfigPath, "client-kubeconfig", "", "kube config for client cluster.")
	flags.StringVar(&ignoreLabels, "ignore-labels", util.BatchPodLabel,
		fmt.Sprintf("ignore-labels are the labels we would like to ignore when build pod for client clusters, "+
			"usually these labels will infulence schedule, default %v, multi labels should be seperated by comma(,"+
			")", util.BatchPodLabel))
	flags.StringVar(&enableControllers, "enable-controllers", "PVControllers,ServiceControllers",
		"support PVControllers,ServiceControllers, default, all of these")

	flags.BoolVar(&enableServiceAccount, "enable-serviceaccount", true,
		"enable service account for pods, like spark driver, mpi launcher")

	logger := logrus.StandardLogger()

	log.L = logruslogger.FromLogrus(logrus.NewEntry(logger))
	logConfig := &logruscli.Config{LogLevel: "info"}

	o, err := opts.FromEnv()
	if err != nil {
		panic(err)
	}
	o.Provider = "k8s"
	o.PodSyncWorkers = numberOfWorkers
	o.Version = strings.Join([]string{k8sVersion, "vk-k8s", buildVersion}, "-")
	o.RateLimiter = rateLimiter()
	node, err := cli.New(ctx,
		cli.WithBaseOpts(o),
		cli.WithProvider("k8s", func(cfg provider.InitConfig) (provider.Provider, error) {
			cfg.ConfigPath = o.KubeConfigPath
			provider, err := k8sprovider.NewVirtualK8S(cfg, &cc, ignoreLabels, enableServiceAccount, o)
			if err == nil {
				go RunController(ctx, provider.GetMaster(),
					provider.GetClient(), provider.GetNameSpaceList(), cfg.NodeName, numberOfWorkers)
			}
			return provider, err
		}),
		cli.WithCLIVersion(buildVersion, buildTime),
		cli.WithKubernetesNodeVersion(k8sVersion),
		// Adds flags and parsing for using logrus as the configured logger
		cli.WithPersistentFlags(logConfig.FlagSet()),
		cli.WithPersistentFlags(flags),
		cli.WithPersistentPreRunCallback(func() error {
			return logruscli.Configure(logConfig, logger)
		}),
	)

	if err != nil {
		log.G(ctx).Fatal(err)
	}

	if err := node.Run(ctx); err != nil {
		log.G(ctx).Fatal(err)
	}
}

// RunController starts controllers for objects needed to be synced
func RunController(ctx context.Context, master,
	client kubernetes.Interface, nsLister corelisters.NamespaceLister, hostIP string,
	workers int) *controllers.ServiceController {

	masterInformer := kubeinformers.NewSharedInformerFactory(master, 0)
	if masterInformer == nil {
		return nil
	}
	clientInformer := kubeinformers.NewSharedInformerFactory(client, 1*time.Minute)
	if clientInformer == nil {
		return nil
	}

	runningControllers := []controllers.Controller{buildCommonControllers(client, masterInformer, clientInformer)}

	controllerSlice := strings.Split(enableControllers, ",")
	for _, c := range controllerSlice {
		if len(c) == 0 {
			continue
		}
		switch c {
		case "PVControllers":
			pvCtrl := buildPVControllers(master, client, masterInformer, clientInformer, hostIP)
			runningControllers = append(runningControllers, pvCtrl)
		case "ServiceControllers":
			serviceCtrl := buildServiceControllers(master, client, masterInformer, clientInformer, nsLister)
			runningControllers = append(runningControllers, serviceCtrl)

		}
	}
	masterInformer.Start(ctx.Done())
	clientInformer.Start(ctx.Done())
	for _, ctrl := range runningControllers {
		go ctrl.Run(workers, ctx.Done())
	}
	<-ctx.Done()
	return nil
}

func buildServiceControllers(master, client kubernetes.Interface, masterInformer,
	clientInformer kubeinformers.SharedInformerFactory,
	nsLister corelisters.NamespaceLister) controllers.Controller {
	// master
	serviceInformer := masterInformer.Core().V1().Services()
	endpointsInformer := masterInformer.Core().V1().Endpoints()
	// client
	clientServiceInformer := clientInformer.Core().V1().Services()
	clientEndpointsInformer := clientInformer.Core().V1().Endpoints()

	serviceRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	endpointsRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	return controllers.NewServiceController(master, client, serviceInformer, endpointsInformer,
		clientServiceInformer, clientEndpointsInformer, nsLister, serviceRateLimiter, endpointsRateLimiter)
}

func buildPVControllers(master, client kubernetes.Interface, masterInformer,
	clientInformer kubeinformers.SharedInformerFactory, hostIP string) controllers.Controller {

	pvcInformer := masterInformer.Core().V1().PersistentVolumeClaims()
	pvInformer := masterInformer.Core().V1().PersistentVolumes()

	clientPVCInformer := clientInformer.Core().V1().PersistentVolumeClaims()
	clientPVInformer := clientInformer.Core().V1().PersistentVolumes()

	pvcRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	pvRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)

	return controllers.NewPVController(master, client, pvcInformer, pvInformer,
		clientPVCInformer,
		clientPVInformer, pvcRateLimiter, pvRateLimiter, hostIP)
}

func buildCommonControllers(client kubernetes.Interface, masterInformer,
	clientInformer kubeinformers.SharedInformerFactory) controllers.Controller {

	configMapInformer := masterInformer.Core().V1().ConfigMaps()
	secretInformer := masterInformer.Core().V1().Secrets()

	clientConfigMapCInformer := clientInformer.Core().V1().ConfigMaps()
	clientSecretInformer := clientInformer.Core().V1().Secrets()

	configMapRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)
	secretRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(time.Second, 30*time.Second)

	return controllers.NewCommonController(client, configMapInformer, secretInformer,
		clientConfigMapCInformer, clientSecretInformer, configMapRateLimiter, secretRateLimiter)
}

func rateLimiter() workqueue.RateLimiter {
	return workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 10*time.Second),
		// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(100), 1000)},
	)
}
