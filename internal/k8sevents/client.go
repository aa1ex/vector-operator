package k8sevents

import (
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"net"
	"strings"
	"time"
)

type client struct {
	protocol  string
	addr      string
	createdAt time.Time
	conn      net.Conn
	stopCh    chan struct{}
	logger    Logger
}

func newClient(protocol, addr string, logger Logger) *client {
	r := client{
		addr:      addr,
		protocol:  protocol,
		createdAt: time.Now(),
		stopCh:    make(chan struct{}),
		logger:    logger,
	}
	return &r
}

func (r *client) reconnect() error {
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.conn = nil
	conn, err := net.Dial(r.protocol, r.addr)
	if err != nil {
		return err
	}
	r.conn = conn
	return nil
}

func (r *client) watchEvents(client rest.Interface, namespace string) {
	watchList := cache.NewListWatchFromClient(client, "events", namespace, fields.Everything())
	_, ctrl := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: watchList,
		ObjectType:    &corev1.Event{},
		ResyncPeriod:  0,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				event := obj.(*corev1.Event)
				r.writeEvent(event)
			},
			UpdateFunc: func(_, obj interface{}) {
				event := obj.(*corev1.Event)
				r.writeEvent(event)
			},
		},
	})
	go ctrl.Run(r.stopCh)
}

func (r *client) writeEvent(event *corev1.Event) {
	if r.conn == nil { // TODO: race condition
		if err := r.reconnect(); err != nil {
			return
		}
	}

	if eventTimestamp(event).Before(r.createdAt) {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		r.logger.Error(err, "marshal event")
		return
	}

	_, err = r.conn.Write(append(data, []byte("\n")...))
	if err != nil {
		if strings.Contains(err.Error(), "broken pipe") {
			err = r.reconnect()
			if err != nil {
				r.logger.Error(err, "reconnect")
			}
			return // TODO(aa1ex): retry send message
		}
		r.logger.Error(err, "write to socket")
		return
	}
}

func (r *client) close() {
	close(r.stopCh)
	if r.conn != nil {
		_ = r.conn.Close()
	}
}
