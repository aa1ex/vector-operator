package k8sevents

import (
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"net"
	"sync"
)

type rec struct {
	*Collector
	net.Conn
}

type EventsManager struct {
	clientset *kubernetes.Clientset

	mutex sync.Mutex
	mp    map[string]rec
}

func NewEventsManager(clientset *kubernetes.Clientset) *EventsManager {
	return &EventsManager{
		mp:        make(map[string]rec),
		clientset: clientset,
	}
}

func (m *EventsManager) AddSubscriber(host, port, namespace string) error {
	tcpSocket, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	collector := NewCollector(m.clientset, []string{namespace}, func(event *corev1.Event) {
		data, err := json.Marshal(event)
		if err != nil {
			// TODO: log
			return
		}
		tcpSocket.Write(data)
	})
	m.mp[host+":"+port+"/"+namespace] = rec{
		Conn:      tcpSocket,
		Collector: collector,
	}
	defer collector.Start()
	return nil
}
