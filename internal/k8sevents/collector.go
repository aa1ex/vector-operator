package k8sevents

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Collector struct {
	namespaces   []string
	clientset    *kubernetes.Clientset
	handler      func(*v1.Event)
	stopChannels []chan struct{}
}

func NewCollector(clientset *kubernetes.Clientset, ns []string, handler func(*v1.Event)) *Collector {
	return &Collector{
		clientset:  clientset,
		namespaces: ns,
		handler:    handler,
	}
}

func (c *Collector) Start() {
	stopCh := make(chan struct{})
	c.stopChannels = append(c.stopChannels, stopCh)
	if len(c.namespaces) == 0 {
		c.watchEvents(v1.NamespaceAll, stopCh)
	} else {
		for _, ns := range c.namespaces {
			c.watchEvents(ns, stopCh)
		}
	}
}

func (c *Collector) Stop() {
	for _, stopperChan := range c.stopChannels {
		close(stopperChan)
	}
}

func (c *Collector) watchEvents(namespace string, stopCh <-chan struct{}) {
	client := c.clientset.CoreV1().RESTClient()
	watchList := cache.NewListWatchFromClient(client, "events", namespace, fields.Everything())
	_, ctrl := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: watchList,
		ObjectType:    &v1.Event{},
		ResyncPeriod:  0,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				event := obj.(*v1.Event)
				c.handler(event)
			},
			UpdateFunc: func(_, obj interface{}) {
				event := obj.(*v1.Event)
				c.handler(event)
			},
		},
	})
	go ctrl.Run(stopCh)
}
