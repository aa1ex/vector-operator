package misc

import (
	"k8s.io/apimachinery/pkg/types"
	"sync"
)

type SecretsToPipelines struct {
	mu   sync.RWMutex
	data map[types.NamespacedName]map[types.NamespacedName]struct{}
}

func NewSecretsToPipelines() *SecretsToPipelines {
	return &SecretsToPipelines{
		mu:   sync.RWMutex{},
		data: make(map[types.NamespacedName]map[types.NamespacedName]struct{}),
	}
}

func (sp *SecretsToPipelines) Get(secret types.NamespacedName) []types.NamespacedName {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	data := sp.data[secret]
	list := make([]types.NamespacedName, 0, len(data))
	for k := range data {
		list = append(list, k)
	}
	return list
}

func (sp *SecretsToPipelines) Add(secret types.NamespacedName, pipeline types.NamespacedName) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.data[secret] == nil {
		sp.data[secret] = make(map[types.NamespacedName]struct{})
	}
	sp.data[secret][pipeline] = struct{}{}
}

func (sp *SecretsToPipelines) Delete(secret types.NamespacedName, pipeline types.NamespacedName) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.data[secret] != nil {
		delete(sp.data[secret], pipeline)
	}
}

func (sp *SecretsToPipelines) DeleteAll(secret types.NamespacedName) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	delete(sp.data, secret)
}
