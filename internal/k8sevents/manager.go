package k8sevents

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"net"
	"strings"
	"sync"
	"time"
)

type rec struct {
	conn   net.Conn
	stopCh chan struct{}
}

type EventsManager struct {
	clientset    *kubernetes.Clientset
	logger       Logger
	debugEnabled bool
	mx           sync.Mutex
	mp           map[string]rec
}

func NewEventsManager(clientset *kubernetes.Clientset, logger Logger, debugEnabled bool) *EventsManager {
	return &EventsManager{
		mp:           make(map[string]rec),
		clientset:    clientset,
		logger:       logger,
		debugEnabled: debugEnabled,
	}
}

func (m *EventsManager) RemoveAllSubscribers() {
	m.mx.Lock()
	defer m.mx.Unlock()
	for k, v := range m.mp {
		v.stopCh <- struct{}{}
		_ = v.conn.Close()
		if v.stopCh != nil {
			close(v.stopCh)
		}
		delete(m.mp, k)
	}
}

func (m *EventsManager) AddSubscriber(host, port, protocol, namespace string) error {
	key := namespace + ":" + host + ":" + port + ":" + protocol

	m.mx.Lock()
	if r, ok := m.mp[key]; ok {
		r.stopCh <- struct{}{}
		_ = r.conn.Close()
		close(r.stopCh)
		delete(m.mp, key)
	}
	m.mx.Unlock()

	stopCh := make(chan struct{})

	var addr string
	if namespace != "" {
		addr = fmt.Sprintf("%s.%s:%s", host, namespace, port)
	} else {
		addr = net.JoinHostPort(host, port)
	}

	conn, err := net.Dial(protocol, addr)
	if err != nil {
		m.mx.Lock()
		delete(m.mp, key)
		m.mx.Unlock()
		return err
	}
	m.mx.Lock()
	m.mp[key] = rec{
		conn:   conn,
		stopCh: stopCh,
	}
	m.mx.Unlock()
	createdAt := time.Now()

	m.watchEvents(namespace, stopCh, func(event *corev1.Event) {
		if eventTimestamp(event).Before(createdAt) {
			return
		}
		data, err := json.Marshal(event)
		if err != nil {
			m.logger.Error(err, "failed to marshal event")
			return
		}

		if m.debugEnabled {
			m.logger.Info("handled event", "event", string(data))
		}

		_, err = conn.Write(append(data, []byte("\n")...))
		if err != nil {
			if strings.Contains(err.Error(), "broken pipe") {
				// TODO: reconnect?
			}
			m.logger.Error(err, "error writing to socket")
			return
		}
	})

	return nil
}

func (m *EventsManager) watchEvents(namespace string, stopCh <-chan struct{}, handler func(event *corev1.Event)) {
	client := m.clientset.CoreV1().RESTClient()
	watchList := cache.NewListWatchFromClient(client, "events", namespace, fields.Everything())
	_, ctrl := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: watchList,
		ObjectType:    &corev1.Event{},
		ResyncPeriod:  0,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				event := obj.(*corev1.Event)
				handler(event)
			},
			UpdateFunc: func(_, obj interface{}) {
				event := obj.(*corev1.Event)
				handler(event)
			},
		},
	})
	go ctrl.Run(stopCh)
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
