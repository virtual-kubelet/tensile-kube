# tensile-kube

## Overview

`tensile-kube` enables kubernetes clusters work together. Based on [virtual-kubelet](https://github.com/virtual-kubelet/virtual-kubelet), `tensile-kube`
provides the following abilities:

- Cluster resource automatically discovery
- Notify pods modification async, decrease the cost of frequently list
- Support all actions of `kubectl log` and `kubectl exec`
- Globally schedule pods to avoid unschedulable pod due to resource fragmentation when using multi-scheduler
- Re-schedule pods if pod can not be scheduled in lower clusters by using descheduler
- PV/PVC
- Service

## Components

- virtual-node

This is a kubernetes provider implemented based on virtual-kubelet. Pods created in the upper cluster
will be synced to the lower cluster. If pods are depend on configmaps or secrets, dependencies would 
also be created in the cluster. 

- multi-scheduler

The scheduler is implemented based on [k8s scheduling framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/). It would watch all of the lower 
clusters's capacity and call `filter` when scheduling pods, it the number of available nodes are more 
than or equal 1, the pod can be scheduler. As you see, this may cost more resources. So we add another 
implementation(descheduler).

- descheduler

descheduler is inspired by [k8s descheduler](https://github.com/kubernetes-sigs/descheduler), but it cannot 
satisfy all our requirements, so we change some logic. Some unschedulable would be re-created by it with some 
nodeAffinity injected.

We can choose one of the multi-scheduler and descheduler in the upper cluster or both.

- webhook

Webhook are design base on k8s mutation webhook. It helps convert some fields that can affect scheduling pods in 
upper cluster, e.g. `nodeSelector`, `nodeAffinity` and `tolerations`. This field would be converted into the
 annotation as follow:
 
```build
    clusterSelector: '{"tolerations":[{"key":"node.kubernetes.io/not-ready","operator":"Exists","effect":"NoExecute"},{"key":"node.kubernetes.io/unreachable","operator":"Exists","effect":"NoExecute"},{"key":"test","operator":"Exists","effect":"NoExecute"},{"key":"test1","operator":"Exists","effect":"NoExecute"},{"key":"test2","operator":"Exists","effect":"NoExecute"},{"key":"test3","operator":"Exists","effect":"NoExecute"}]}'
``` 

This fields we would be added back when the pods created in the lower cluster.

## Restrictions

- If you want to use service, must keep inter-pods communication normal. Pod A in 
cluster B can be accessed by pod B in cluster B through ip. The service `kubernetes` 
in `default` namespaces and other services in `kube-system` would be synced to lower clusters.

- multi-scheduler developed in the repo may cost more resource because it would sync all objects
that a scheduler need from all of lower clusters.

- descheduler cannot absolutely avoid resource fragmentation.

- PV/PVC only support `WaitForFirstConsumer`for local PV, the scheduler in the upper cluster should ignore
 `VolumeBindCheck`

## Use Case

![multi](./docs/multi.png)

In [Tencent Games](https://game.qq.com/), we build the kubernetes cluster based on the [flannel](https://github.com/coreos/flannel) and all node CIDR allocation
 is based on the same etcd. So the pods actually can access each other directly by IP.

## Build

```build
git clone https://github.com/virtual-kubelet/tensile-kube.git && make
```
### virtual node parameters

```build
      --client-burst int           qpi burst for client cluster. (default 1000)
      --client-kubeconfig string   kube config for client cluster.
      --client-qps int             qpi qps for client cluster. (default 500)
      --enableControllers string   support PVControllers,RBACControllers,ServiceControllers, default, all of these (default "PVControllers,RBACControllers,ServiceControllers")
      --ignore-labels string       ignore-labels are the labels we would like to ignore when build pod for client clusters, usually these labels will infulence schedule, default group.batch.scheduler.tencent.com, multi labels should be seperated by comma(,) (default "group.batch.scheduler.tencent.com")
      --log-level string           set the log level, e.g. "debug", "info", "warn", "error" (default "info")
```

### deploy the virtual node(support deploy in cluster)

```build
export KUBELET_PORT=10350
export APISERVER_CERT_LOCATION=/etc/k8s.cer
export APISERVER_KEY_LOCATION=/etc/k8s.key

nohup ./virtual-node --nodename $IP --provider k8s --kube-api-qps 500 --kube-api-burst 1000 --client-qps 500 --client
-burst 1000 --kubeconfig /root/server-kube.config --client-kubeconfig /client-kube.config --klog.v 4 --log-level
 debug 2>&1 > node.log &
```

### deploy the multi-scheduler(support deploy in cluster)

```build
nohup ./multi-scheduler --v=5 --config=./multi-scheduler-config.json --authentication-kubeconfig=/etc/kubernetes/scheduler.conf --authorization-kubeconfig=/etc/kubernetes/scheduler.conf --bind-address=127.0.0.1 --kubeconfig=/etc/kubernetes/scheduler.conf --leader-elect=true 2>&1 >multi.log &
```

multi-scheduler-config.json is as follow:

```build
{
  "kind": "KubeSchedulerConfiguration",
  "apiVersion": "kubescheduler.config.k8s.io/v1alpha1",
  "clientConnection": {
    "kubeconfig": "~/root/kube/config"
  },
  "leaderElection": {
    "leaderElect": true
  },
  "plugins": {
    "filter": {
      "enabled": [
        {
          "name": "multi-scheduler"
        }
      ]
    }
  },
  "pluginConfig": [
    {
      "name": "multi-scheduler",
      "args": {
        "cluster_configuration": {
          "test": {
            "kube_config": "/root/cloud.config"
          }
        }
      }
    }
  ]
}
```

### deploy the webhook

it is recommended to be deployed in k8s cluster

1. replace the ${caBoudle}, ${cert}, ${key} with yours
2. replace the image of webhook
3. deploy it in k8s cluster
```build
kubectl apply -f hack/webhook.yaml
```

### deploy the descheduler

1. replace the image with yours
2. deploy it in k8s cluster
```build
kubectl apply -f hack/descheduler.yaml
```

## Contributors

- [Weidong Cai](https://github.com/cwdsuzhou) from Tencent Games
- [Ye Yin](https://github.com/hustcat) from Tencent Games
