package k8sevents

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net"
	"sync"
	"time"
)

type EventsManager struct {
	client rest.Interface
	logger Logger
	mx     sync.Mutex
	mp     map[string]*client
}

func NewEventsManager(clientset *kubernetes.Clientset, logger Logger) *EventsManager {
	return &EventsManager{
		mp:     make(map[string]*client),
		client: clientset.CoreV1().RESTClient(),
		logger: logger,
	}
}

func (m *EventsManager) RegisterSubscriber(host, port, protocol, namespace string) {
	key := namespace + ":" + host + ":" + port + ":" + protocol

	addr := net.JoinHostPort(host, port)
	c := newClient(protocol, addr, m.logger)

	m.mx.Lock()
	if oldC, ok := m.mp[key]; ok {
		oldC.close()
	}
	m.mp[key] = c
	m.mx.Unlock()

	c.watchEvents(m.client, namespace)
}

func eventTimestamp(ev *corev1.Event) time.Time {
	var ts time.Time
	switch {
	case ev.EventTime.Time != time.Time{}:
		ts = ev.EventTime.Time
	case ev.LastTimestamp.Time != time.Time{}:
		ts = ev.LastTimestamp.Time
	case ev.FirstTimestamp.Time != time.Time{}:
		ts = ev.FirstTimestamp.Time
	}
	return ts
}
